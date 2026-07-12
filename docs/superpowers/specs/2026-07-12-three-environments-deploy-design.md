# Three deployment environments for mediumrogue

**Status:** design approved 2026-07-12, awaiting spec review
**Pattern source:** `starquake/topbanana` (`.github/workflows/{ci,deploy}.yml`,
`deployments/app/*`, `deployments/swag/*`)

## Goal

Stand up three independently-worlded deployments of the mediumrogue binary,
each on its own subdomain, fed by three different triggers:

| Environment    | Domain                                    | Trigger                                                    | Image                                              |
| -------------- | ----------------------------------------- | --------------------------------------------------------- | -------------------------------------------------- |
| **production** | `mediumrogue.bananajuice.net`             | push a `v*.*.*` git tag                                    | `promote` retags tested `sha-<commit>` → semver    |
| **staging**    | `mediumrogue-staging.bananajuice.net`     | CI green on `main` (each squash-merge = one main commit)   | `:edge`, cosign-verified                           |
| **development**| `mediumrogue-development.bananajuice.net` | add the `deploy:dev` label to a PR (redeploys while labeled)| `pr-<n>` built in the deploy job, **no cosign**    |

All three run on the **same VPS as topbanana**, each as its own container on the
shared external `web` docker network, each with its **own named volume** — three
fully independent world states. SWAG (the existing nginx-based reverse proxy)
terminates TLS and routes the three subdomains. No container publishes a host
port; SWAG reaches each one over the `web` network.

## Why this shape

mediumrogue is one self-contained Go binary with the client embedded
(`go:embed`), containerized by the existing `Dockerfile`. It is **stateful** —
an in-memory world plus an optional JSON snapshot (`SNAPSHOT_PATH`) — but has
**no secrets**: no database, no OAuth, no SMTP, no session key. So we copy
topbanana's build-once → sign → promote → deploy-by-digest pipeline and delete
its entire `.env`/secrets apparatus. The only genuinely new piece versus
topbanana is the development environment, which tracks a **labeled PR** instead
of `main`.

`GET /healthz` already exists on the server and is used as the deploy health
check.

## Non-goals

- No database, media, SMTP, OAuth, or session-key wiring (topbanana has these;
  mediumrogue does not).
- No scheduled world reset (topbanana's `demo-reset.yml` has no analog here).
- No automatic teardown of the development container when a PR is unlabeled or
  closed — the dev slot simply holds the last labeled PR until replaced. (A
  teardown-on-close job is a possible later addition, out of scope here.)
- No blue/green or zero-downtime rollout — `docker compose up -d` recreate is
  acceptable for a ~15-friend game.

## Component 1 — `ci.yml` additions (image pipeline)

Today `.github/workflows/ci.yml` runs `lint`, `test`, `client`, `e2e` on
`push: [main]` and `pull_request`. It builds **no image**. Changes:

- **Triggers:** keep `push: branches: [main]` and `pull_request`; add
  `push: tags: ['v*.*.*']`.
- **Gate the existing four jobs** with `if: ${{ !startsWith(github.ref, 'refs/tags/') }}`
  so cutting a release tag does not re-run the suite (it already ran when the
  commit landed on `main`).
- **New `docker-build` job** — `needs: [lint, test, client, e2e]`, same tag
  guard. Permissions `contents: read`, `packages: write`, `id-token: write`.
  Steps mirror topbanana:
  - `docker/setup-buildx-action`, `docker/login-action` to `ghcr.io` (skip
    login on PR), `sigstore/cosign-installer` (skip on PR).
  - `docker/metadata-action` tags: `type=sha,format=long` and
    `type=edge,branch=main`; `flavor: latest=auto`.
  - `docker/build-push-action` with `context: .`, `push: ${{ github.event_name != 'pull_request' }}`,
    `cache-from/to: type=gha`. Uses the existing `Dockerfile` unchanged.
  - cosign keyless `sign --yes` the pushed digest (skip on PR).
- **New `promote` job** — `if: startsWith(github.ref, 'refs/tags/v')`.
  Permissions `contents: read`, `packages: write`. `docker/metadata-action`
  with `type=semver,pattern={{version}}` and `{{major}}.{{minor}}`,
  `flavor: latest=auto`. Then `docker buildx imagetools create --tag <each>
  ghcr.io/<repo>:sha-<sha>`. Fails loudly if the source `sha-<sha>` image is
  missing (a `v*` tag must sit on a commit whose `main` build pushed its sha
  image). No rebuild; the cosign signature binds to the digest and rides along.

Because PRs are squash-merged to `main`, every release tag lands on a `main`
commit that already has a signed `sha-<commit>` image — `promote` always has a
source to retag.

## Component 2 — `deploy.yml` (new)

Triggers:
```yaml
on:
  workflow_run:
    workflows: ["CI"]
    types: [completed]
  workflow_dispatch:
    inputs:
      environment: { type: choice, options: [staging, production] }
  pull_request:
    types: [labeled, synchronize]
```
`workflow_dispatch` covers only staging/production (redeploy the current
`:edge` or a chosen version). Development is **label-only**: a manual dispatch
has no PR context to build a `pr-<n>` image from, so it is not offered there.

`env: REGISTRY: ghcr.io`, `IMAGE_NAME: ${{ github.repository }}`. Three jobs,
each bound to a GitHub Environment (`environment: { name: <env>, url: ${{ vars.SERVER_URL }} }`),
each with permissions `contents: read`, `packages: read` (dev adds
`packages: write` to push its PR image).

### deploy-staging
Fires when CI succeeded on a push to `main`:
```
github.event_name == 'workflow_run' && event.workflow_run.conclusion == 'success'
  && event.workflow_run.event == 'push'
  && event.workflow_run.head_repository.full_name == github.repository
  && event.workflow_run.head_branch == 'main'
|| (github.event_name == 'workflow_dispatch' && inputs.environment == 'staging')
```
Steps: checkout at `head_sha`; scp `deployments/app/docker-compose.staging.yml`
to `~/mediumrogue-staging`; buildx; ghcr login; cosign install; resolve
`:edge` → digest (regex-validate `sha256:[0-9a-f]{64}`); **cosign verify** with
`--certificate-identity-regexp '^https://github\.com/starquake/mediumrogue/\.github/workflows/ci\.yml@refs/heads/main$'`
and `--certificate-oidc-issuer 'https://token.actions.githubusercontent.com'`;
SSH deploy (below); health-check poll of `${{ secrets.HEALTH_URL }}` (12×5s).

### deploy-production
Fires when CI succeeded on a `v*` tag:
```
... event.workflow_run.head_branch startsWith 'v'
|| (workflow_dispatch && inputs.environment == 'production')
```
Same shape; "determine image tag" strips the leading `v` and validates
`^[0-9]+\.[0-9]+\.[0-9]+$` (else `latest`); resolves the **semver tag** →
digest; same cosign verify; deploys `docker-compose.production.yml` to
`~/mediumrogue-production`.

### deploy-development
Fires when a PR carries the `deploy:dev` label:
```
github.event_name == 'pull_request'
  && contains(github.event.pull_request.labels.*.name, 'deploy:dev')
  && github.event.pull_request.head.repo.full_name == github.repository
```
(Label-only; no `workflow_dispatch` branch — a manual run has no PR to build.)
Permissions add `packages: write`. `concurrency: { group: deploy-development,
cancel-in-progress: true }` so rapid pushes to the labeled PR coalesce to the
newest. Steps: checkout the PR head; buildx; ghcr login; **build & push**
`ghcr.io/<repo>:pr-<number>` from the existing `Dockerfile`; resolve that tag →
digest; **skip cosign**; deploy `docker-compose.development.yml` to
`~/mediumrogue-development`; health-check. Not gated on green CI — it is a fast
preview sandbox. Fork PRs are excluded by the `head.repo` guard (single-author
repo, so this is belt-and-suspenders).

### SSH deploy step (all three, secret-free)
`appleboy/scp-action` already copied the compose file. Then
`appleboy/ssh-action` forwards only `IMAGE_NAME`, `IMAGE_DIGEST` (and
`GITHUB_TOKEN`/`GITHUB_ACTOR` for the pull login) via `envs:`. The script:
```
set -e
mkdir -p ~/mediumrogue-<env> && cd ~/mediumrogue-<env>
umask 077
cat > .env <<EOF
IMAGE_NAME='${IMAGE_NAME}'
IMAGE_DIGEST='${IMAGE_DIGEST}'
EOF
mv -f docker-compose.<env>.yml docker-compose.yml
echo "$GITHUB_TOKEN" | docker login ghcr.io -u "$GITHUB_ACTOR" --password-stdin
docker compose pull
docker compose up -d
docker logout ghcr.io
```
No `SESSION_KEY`/OAuth/SMTP guards — there are no secrets to guard.

## Component 3 — compose files `deployments/app/docker-compose.<env>.yml`

One per environment. Shared shape (values differ per env where noted):
```yaml
services:
  mediumrogue-<env>:
    image: ${IMAGE_NAME}@${IMAGE_DIGEST:?IMAGE_DIGEST must be set in .env}
    container_name: mediumrogue-<env>
    environment:
      - LISTEN_ADDR=0.0.0.0:8080
      - TURN_INTERVAL=${TURN_INTERVAL:-4s}          # production cadence
      - HEARTBEAT_INTERVAL=${HEARTBEAT_INTERVAL:-15s}
      - MONSTER_COUNT=${MONSTER_COUNT:-40}          # tune per env
      - WORLD_SEED=${WORLD_SEED:-}                  # empty → config default
      - SNAPSHOT_PATH=/home/nonroot/data/world.json
      - SNAPSHOT_INTERVAL=${SNAPSHOT_INTERVAL:-60s}
    volumes:
      - mediumrogue_<env>_data:/home/nonroot/data
    networks: [web]
    restart: unless-stopped
volumes:
  mediumrogue_<env>_data:
networks:
  web:
    external: true
```
Notes:
- Image pinned to the immutable digest resolved by `deploy.yml`, never a
  mutable tag — a later tag move cannot swap the running bytes.
- Distroless-static app filesystem is ephemeral; the snapshot must live on the
  named volume (`/home/nonroot/data`), matching topbanana's data-dir choice.
- Each env's own volume → independent worlds. Dev/staging may run more monsters
  or a shorter turn for testing; production keeps the protocol default cadence.
- No `ports:` — SWAG reaches the container by name on the `web` network.
- Env values not written into the host `.env` fall back to the compose
  defaults shown; the deploy `.env` only carries `IMAGE_NAME`/`IMAGE_DIGEST`.
  (To tune a knob per env without touching the workflow, set it directly in
  that env's committed compose file.)

## Component 4 — SWAG confs `deployments/swag/mediumrogue-<env>.subdomain.conf`

Three nginx server blocks, one per subdomain, each proxying to its container on
port 8080. **SSE tuning is the one real gotcha:** the `/api/events` stream must
not be buffered or it stalls behind nginx's response buffer, and it needs a long
read timeout and HTTP/2 (SSE over HTTP/1.1 also eats the browser's per-domain
connection limit — see plan §7). Shape:
```nginx
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name mediumrogue-<env>.*;   # production block uses `mediumrogue.*`
    include /config/nginx/ssl.conf;

    location / {
        include /config/nginx/proxy.conf;
        include /config/nginx/resolver.conf;
        set $upstream_app mediumrogue-<env>;
        set $upstream_port 8080;
        set $upstream_proto http;
        proxy_pass $upstream_proto://$upstream_app:$upstream_port;
    }

    location /api/events {
        include /config/nginx/proxy.conf;
        include /config/nginx/resolver.conf;
        set $upstream_app mediumrogue-<env>;
        set $upstream_port 8080;
        set $upstream_proto http;
        proxy_pass $upstream_proto://$upstream_app:$upstream_port;
        proxy_buffering off;            # flush turn-bundles immediately
        proxy_cache off;
        proxy_read_timeout 1h;          # long-lived stream
        proxy_set_header Connection '';
    }
}
```
`server_name mediumrogue.*` (production) does not collide with
`mediumrogue-staging.*` / `mediumrogue-development.*` — the `.` after
`mediumrogue` means the production pattern only matches `mediumrogue.<tld>`,
while the others start with `mediumrogue-`.

## Component 5 — HOST / GITHUB SETUP (operator does this)

> **This is the "server side" the workflow cannot do for you.** The PR delivers
> the workflow + compose + SWAG confs; these steps make them live. Grouped so
> it's clear what is manual.

**DNS (you):** three CNAMEs pointing at the VPS —
`mediumrogue`, `mediumrogue-staging`, `mediumrogue-development` under
`bananajuice.net`.

**GitHub (you):** create three **Environments** — `production`, `staging`,
`development`. On each set:
- Secrets (shared across all three, same VPS): `SSH_HOST`, `SSH_USER`,
  `SSH_KEY`, `HEALTH_URL` (the env's `https://…/healthz`).
- Variables: `SERVER_URL` (the env's public URL, shown on the Environment
  deployment card).
- No app secrets — mediumrogue has none.

**Host (you):** place the three `mediumrogue-*.subdomain.conf` files in SWAG's
`proxy-confs` dir and reload SWAG. Ensure the external `web` docker network
exists (it already does for topbanana). Ensure the SSH user can run `docker`.

**Registry (you):** the `ghcr.io/starquake/mediumrogue` package must be pullable
by the host's `docker login` (the deploy uses `GITHUB_TOKEN`; confirm package
visibility/permissions on first deploy).

**Optional (you):** create the `deploy:dev` label in the repo (GitHub also
creates it on first use).

## Divergences from topbanana (summary)

- **No secrets:** the `.env` heredoc shrinks to `IMAGE_NAME`/`IMAGE_DIGEST`; no
  `SESSION_KEY`, Google OAuth, SMTP, or their compose `${VAR:?}` guards.
- **Third env is a labeled PR, not a demo tracking main.** It builds a `pr-<n>`
  image in the deploy job and skips cosign; it is not gated on green CI.
- **State is a JSON snapshot volume, not SQLite/media.**
- **SSE proxy tuning** (`proxy_buffering off`, long read timeout, http2) is
  required in the SWAG confs; topbanana's HTTP request/response app did not
  need it.
- No `demo-reset.yml` scheduled job.

## Testing / verification

- `ci.yml` and `deploy.yml` YAML validated (`actionlint` if available; at
  minimum GitHub's own parse on push).
- First real proof is end-to-end on the host: push to `main` → staging comes up
  and `/healthz` passes; tag `v0.0.1` → production; label a PR `deploy:dev` →
  development. Each verified by the workflow's own health-check step plus a
  manual load of the subdomain.
- No Go/TS code changes, so `make check` is unaffected; the existing suite
  still gates staging/production via CI.

## Open items to confirm at implementation time

- Exact per-env knob values (`MONSTER_COUNT`, `TURN_INTERVAL`, `WORLD_SEED`) —
  placeholders above; pick real numbers when writing the compose files.
- Whether to also run `make check`'s FEATURES.md update: this PR adds ops
  infrastructure, not a game mechanic or config var consumed by the binary, so
  FEATURES.md likely needs only a short "Deployments" note, not a table change.
