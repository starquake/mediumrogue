# Deployments

Three environments of the mediumrogue binary, one VPS, behind the existing
SWAG reverse proxy. All automation lives in `.github/workflows/{ci,deploy}.yml`;
this file is the **one-time manual setup** the workflows can't do.

| Environment  | Domain                                    | Trigger                          |
| ------------ | ----------------------------------------- | -------------------------------- |
| production   | `mediumrogue.bananajuice.net`             | push a `v*.*.*` git tag          |
| staging      | `mediumrogue-staging.bananajuice.net`     | CI green on `main`               |
| development  | `mediumrogue-development.bananajuice.net` | `deploy:dev` label on a PR       |

## One-time setup (operator)

### 1. DNS (Hetzner)
Add three CNAMEs to `bananajuice.net`, all pointing at the VPS:
`mediumrogue`, `mediumrogue-staging`, `mediumrogue-development`.

### 2. TLS (SWAG)
Staying on HTTP-01 / enumerated hostnames (no wildcard, no DNS credential).
Add the three names to SWAG's `SUBDOMAINS` env var; SWAG re-issues the
multi-SAN cert automatically.

### 3. SWAG proxy confs
Copy the three `deployments/swag/mediumrogue*.subdomain.conf` files into
SWAG's `proxy-confs/` directory, then reload:
`docker exec <swag-container> nginx -s reload` (this is where the confs are
actually validated — `nginx -t` needs SWAG's include files).
Ensure the external `web` docker network exists (it already does for
topbanana) and the SSH user can run `docker`.

### 4. GitHub Environments
Create three environments — `production`, `staging`, `development` — under
repo Settings → Environments. On **each**, set:

- Secrets (same values across all three — one VPS):
  - `SSH_HOST`, `SSH_USER`, `SSH_KEY` — SSH access to the VPS.
  - `HEALTH_URL` — that environment's `https://<domain>/healthz`.
- Variables:
  - `SERVER_URL` — that environment's public URL (shown on the deployment card).

No application secrets — mediumrogue has none.

### 5. GHCR package
On the first `main` push, CI publishes `ghcr.io/starquake/mediumrogue`.
Confirm the VPS can `docker login ghcr.io` and pull it (the deploy uses the
Actions `GITHUB_TOKEN`; check the package's visibility/permissions).

### 6. Development label
Create a `deploy:dev` label in the repo (GitHub also auto-creates it on first
use). Add it to a PR to deploy that PR to development.

**The PR's branch must be current with `main`.** `pull_request`-triggered
workflows run from the *PR branch's* copy of the workflow file, so a branch
that predates the deploy pipeline (no `.github/workflows/deploy.yml`) will not
fire on the label — nothing happens. Merge `main` into the branch first, then
apply (or re-apply) the label.

## Manual redeploy
`Actions → Deploy → Run workflow` offers a `staging`/`production` choice
(re-deploys the current `:edge` or the latest release). Development is
label-only.

## Per-environment world knobs
Each `deployments/app/docker-compose.<env>.yml` sets `TURN_INTERVAL`,
`MONSTER_COUNT`, etc. directly. Edit the file and redeploy to retune one
environment; the deploy `.env` only carries `IMAGE_NAME`/`IMAGE_DIGEST`.
Each environment has its own `mediumrogue_<env>_data` volume, so worlds
never share state.
