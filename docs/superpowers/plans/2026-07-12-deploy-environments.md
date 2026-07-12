# Three Deployment Environments Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship production/staging/development deployments of the mediumrogue binary, each on its own subdomain with its own world state, driven by tag / main-commit / labeled-PR triggers.

**Architecture:** Copy topbanana's build-once → cosign-sign → promote → deploy-by-digest pipeline, stripped of all secrets (mediumrogue has none). CI builds and signs one image per green main commit; a release tag retags it; a deploy workflow SSHes into the shared VPS, pins each environment's compose file to the verified digest, and runs `docker compose up -d` behind the existing SWAG reverse proxy. The development environment is the one divergence: it builds a `pr-<n>` image from a labeled PR and skips signature verification.

**Tech Stack:** GitHub Actions, GHCR (ghcr.io), Docker Buildx + Compose, cosign (keyless/sigstore), appleboy scp/ssh actions, SWAG (nginx). No application-code changes — the existing `Dockerfile` and `/healthz` endpoint are reused as-is.

**Spec:** `docs/superpowers/specs/2026-07-12-three-environments-deploy-design.md`

## Global Constraints

- **Registry / image:** `ghcr.io/${{ github.repository }}` → `ghcr.io/starquake/mediumrogue` (already lowercase).
- **No application secrets.** The host `.env` written by every deploy job contains exactly `IMAGE_NAME` and `IMAGE_DIGEST`. Never add `SESSION_KEY`/OAuth/SMTP — mediumrogue has none.
- **Pin to digest, never a tag.** Compose images use `${IMAGE_NAME}@${IMAGE_DIGEST:?...}`; the deploy job resolves the mutable tag to its immutable digest first.
- **cosign identity (exact):** `^https://github\.com/starquake/mediumrogue/\.github/workflows/ci\.yml@refs/heads/main$`, OIDC issuer `https://token.actions.githubusercontent.com`. Used for staging + production; **skipped for development**.
- **Snapshot volume path is `/data`** (the `Dockerfile` runs as root, no `USER` directive). Do NOT use topbanana's `/home/nonroot/data`.
- **Health check:** poll `GET /healthz` (already implemented) 12× at 5s.
- **SSE de-buffering is mandatory** in the SWAG confs: `/api/events` needs `proxy_buffering off` + a long `proxy_read_timeout`, or turn-bundles stall behind nginx's buffer.
- **Pinned action versions** (match topbanana): `actions/checkout@v7`, `docker/setup-buildx-action@v4`, `docker/login-action@v4`, `docker/metadata-action@v6`, `docker/build-push-action@v7`, `sigstore/cosign-installer@v4.1.2` (cosign `v2.2.4`), `appleboy/scp-action@v1.0.0`, `appleboy/ssh-action@v1.2.5`.
- **Container / volume / compose-file naming:** `mediumrogue-<env>` container, `mediumrogue_<env>_data` volume, `docker-compose.<env>.yml`, host dir `~/mediumrogue-<env>`, where `<env>` ∈ {production, staging, development}.
- **Development label:** `deploy:dev`.

**Validation tooling used by this plan:**
- Workflows: `actionlint` via `go run github.com/rhysd/actionlint/cmd/actionlint@latest <file>` (Go is on PATH via the Makefile fallback; this needs one-time network to fetch the module). Expected output on success: no output, exit 0.
- Compose: `docker compose -f <file> config -q` with dummy env, expected exit 0.
- Nginx confs: structural check (the real `nginx -t` needs SWAG's `/config/nginx/*` includes, so it only validates on the host at SWAG reload — noted per task).
- No Go/TS source changes, so `make check` remains green throughout; a final task confirms it.

---

### Task 1: CI image pipeline — build + sign + promote

Adds image production to `.github/workflows/ci.yml`: the four existing jobs are gated off release tags, a `docker-build` job builds/signs the image after the suite is green, and a `promote` job retags it on a `v*` tag.

**Files:**
- Modify: `.github/workflows/ci.yml` (whole file replaced with the content below)

**Interfaces:**
- Produces: image tags in GHCR — `sha-<commit>` (always on non-PR), `:edge` (main only), and (on `v*` tags) `{{version}}`, `{{major}}.{{minor}}`, `latest`. Task 3's deploy jobs consume `:edge` (staging) and the semver tag (production). The cosign signature is bound to the digest with identity `ci.yml@refs/heads/main`.

- [ ] **Step 1: Replace `.github/workflows/ci.yml` with this exact content**

```yaml
name: CI

on:
  push:
    branches: [main]
    # Release tags promote the already-built main image; they do not
    # re-run the suite or rebuild (see the promote job).
    tags: ['v*.*.*']
  pull_request:

env:
  REGISTRY: ghcr.io
  IMAGE_NAME: ${{ github.repository }}

permissions:
  contents: read

jobs:
  lint:
    if: ${{ !startsWith(github.ref, 'refs/tags/') }}
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      # version MUST match GOLANGCI_VERSION in the Makefile — bump both together.
      - uses: golangci/golangci-lint-action@v9
        with:
          version: v2.12.2

  test:
    if: ${{ !startsWith(github.ref, 'refs/tags/') }}
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - run: make test test-integration

  client:
    if: ${{ !startsWith(github.ref, 'refs/tags/') }}
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - uses: actions/setup-node@v6
        with:
          node-version: 24
          cache: npm
          cache-dependency-path: client/package-lock.json
      # The contract-drift gate: regenerates protocol.gen.ts and fails on diff.
      - run: make protocol-check
      - run: make client-check
      - run: make client

  e2e:
    if: ${{ !startsWith(github.ref, 'refs/tags/') }}
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - uses: actions/setup-node@v6
        with:
          node-version: 24
          cache: npm
          cache-dependency-path: client/package-lock.json
      - run: cd client && npm ci && npx playwright install --with-deps chromium
      - run: make e2e

  # Build once, after the suite is green, so a published image always
  # implies the tests passed for that commit. Release tags do not build
  # here; the promote job retags this image instead.
  docker-build:
    needs: [lint, test, client, e2e]
    if: ${{ !startsWith(github.ref, 'refs/tags/') }}
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
      # Needed for cosign keyless signing (sigstore/fulcio OIDC) off PRs.
      id-token: write
    steps:
      - name: Checkout repository
        uses: actions/checkout@v7

      - name: Install cosign
        if: github.event_name != 'pull_request'
        uses: sigstore/cosign-installer@v4.1.2
        with:
          cosign-release: 'v2.2.4'

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v4

      - name: Log into registry ${{ env.REGISTRY }}
        if: github.event_name != 'pull_request'
        uses: docker/login-action@v4
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      # No semver tags here: version tags are created by the promote job.
      - name: Extract Docker metadata
        id: meta
        uses: docker/metadata-action@v6
        with:
          images: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}
          tags: |
            type=sha,format=long
            type=edge,branch=main
          flavor: |
            latest=auto

      # Build (and, off PRs, push) the image using the existing Dockerfile.
      - name: Build and push Docker image
        id: build-and-push
        uses: docker/build-push-action@v7
        with:
          context: .
          push: ${{ github.event_name != 'pull_request' }}
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
          cache-to: type=gha,mode=max

      # Sign the pushed digest. The signature binds to the digest, so the
      # promote job's retags inherit it.
      - name: Sign the published Docker image
        if: ${{ github.event_name != 'pull_request' }}
        env:
          TAGS: ${{ steps.meta.outputs.tags }}
          DIGEST: ${{ steps.build-and-push.outputs.digest }}
        run: echo "${TAGS}" | xargs -I {} cosign sign --yes "{}@${DIGEST}"

  # Promote the already-built, already-tested main image for this commit to
  # its release tags. No rebuild, no re-run of the suite: the tag is cut on a
  # main commit, so its sha-<commit> image already exists and was signed.
  promote:
    if: startsWith(github.ref, 'refs/tags/v')
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
    steps:
      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v4

      - name: Log into registry ${{ env.REGISTRY }}
        uses: docker/login-action@v4
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Extract Docker metadata
        id: meta
        uses: docker/metadata-action@v6
        with:
          images: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}
          tags: |
            type=semver,pattern={{version}}
            type=semver,pattern={{major}}.{{minor}}
          flavor: |
            latest=auto

      - name: Promote main image to release tags
        env:
          SRC: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}:sha-${{ github.sha }}
          TAGS: ${{ steps.meta.outputs.tags }}
        run: |
          set -euo pipefail
          # Fail loudly if the tested main image is missing: a v* tag must
          # point at a commit whose main build pushed its sha image.
          if ! docker buildx imagetools inspect "$SRC" >/dev/null 2>&1; then
            echo "ERROR: ${SRC} not found; tag a commit that was built on main" >&2
            exit 1
          fi
          args=()
          while IFS= read -r tag; do
            [ -n "$tag" ] && args+=( --tag "$tag" )
          done <<< "$TAGS"
          docker buildx imagetools create "${args[@]}" "$SRC"
```

- [ ] **Step 2: Lint the workflow**

Run: `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/ci.yml`
Expected: no output, exit 0. (If actionlint reports "shellcheck not found", that is a warning about the inline script lint, not a failure — the exit code is what matters.)

- [ ] **Step 3: Sanity-check the gating logic by eye**

Confirm all four of `lint`/`test`/`client`/`e2e` and `docker-build` carry `if: ${{ !startsWith(github.ref, 'refs/tags/') }}`, and `promote` carries `if: startsWith(github.ref, 'refs/tags/v')`. These are mutually exclusive: a tag push runs only `promote`; a branch/PR runs everything except `promote`.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: build, sign, and promote container images for deploys"
```

---

### Task 2: Per-environment compose files

Three compose files, one per environment, identical except for names and (optionally) tuned world knobs. Each pins the image to the digest the deploy job resolves and mounts its own named volume so the three worlds never share state.

**Files:**
- Create: `deployments/app/docker-compose.production.yml`
- Create: `deployments/app/docker-compose.staging.yml`
- Create: `deployments/app/docker-compose.development.yml`

**Interfaces:**
- Consumes: `.env` with `IMAGE_NAME` and `IMAGE_DIGEST` (written by Task 3's deploy jobs).
- Produces: a running `mediumrogue-<env>` container on the external `web` network, port 8080, snapshot on `mediumrogue_<env>_data:/data`. Task 4's SWAG confs proxy to these container names.

- [ ] **Step 1: Create `deployments/app/docker-compose.production.yml`**

```yaml
# Production. Pinned to the immutable digest deploy.yml resolves + verifies;
# the ${IMAGE_DIGEST:?} guard fails the boot loudly if it is unset. No host
# port: SWAG reaches the container by name on the shared `web` network.
services:
  mediumrogue-production:
    image: ${IMAGE_NAME}@${IMAGE_DIGEST:?IMAGE_DIGEST must be set in .env or environment}
    container_name: mediumrogue-production
    environment:
      - LISTEN_ADDR=0.0.0.0:8080
      - TURN_INTERVAL=4s
      - HEARTBEAT_INTERVAL=15s
      - MONSTER_COUNT=40
      # World snapshot lives on the named volume: the distroless app
      # filesystem is ephemeral, so a snapshot elsewhere is lost on redeploy.
      - SNAPSHOT_PATH=/data/world.json
      - SNAPSHOT_INTERVAL=60s
    volumes:
      - mediumrogue_production_data:/data
    networks:
      - web
    restart: unless-stopped

volumes:
  mediumrogue_production_data:
networks:
  web:
    external: true
```

- [ ] **Step 2: Create `deployments/app/docker-compose.staging.yml`**

```yaml
# Staging. Tracks main via the :edge image (deploy.yml resolves it to a
# verified digest). Same shape as production, separate world + volume.
services:
  mediumrogue-staging:
    image: ${IMAGE_NAME}@${IMAGE_DIGEST:?IMAGE_DIGEST must be set in .env or environment}
    container_name: mediumrogue-staging
    environment:
      - LISTEN_ADDR=0.0.0.0:8080
      - TURN_INTERVAL=4s
      - HEARTBEAT_INTERVAL=15s
      - MONSTER_COUNT=40
      - SNAPSHOT_PATH=/data/world.json
      - SNAPSHOT_INTERVAL=60s
    volumes:
      - mediumrogue_staging_data:/data
    networks:
      - web
    restart: unless-stopped

volumes:
  mediumrogue_staging_data:
networks:
  web:
    external: true
```

- [ ] **Step 3: Create `deployments/app/docker-compose.development.yml`**

```yaml
# Development. Runs whatever PR carries the deploy:dev label; image is a
# pr-<n> build (unsigned). Separate world + volume; safe to wipe anytime.
services:
  mediumrogue-development:
    image: ${IMAGE_NAME}@${IMAGE_DIGEST:?IMAGE_DIGEST must be set in .env or environment}
    container_name: mediumrogue-development
    environment:
      - LISTEN_ADDR=0.0.0.0:8080
      - TURN_INTERVAL=4s
      - HEARTBEAT_INTERVAL=15s
      - MONSTER_COUNT=40
      - SNAPSHOT_PATH=/data/world.json
      - SNAPSHOT_INTERVAL=60s
    volumes:
      - mediumrogue_development_data:/data
    networks:
      - web
    restart: unless-stopped

volumes:
  mediumrogue_development_data:
networks:
  web:
    external: true
```

- [ ] **Step 4: Validate all three parse**

Run:
```bash
DIGEST="sha256:$(printf '0%.0s' $(seq 1 64))"
for e in production staging development; do
  IMAGE_NAME=ghcr.io/starquake/mediumrogue IMAGE_DIGEST="$DIGEST" \
    docker compose -f "deployments/app/docker-compose.$e.yml" config -q \
    && echo "$e OK"
done
```
Expected: `production OK`, `staging OK`, `development OK` (three lines), exit 0. `config -q` prints nothing on success; the `&& echo` confirms each. (An external network named `web` need not exist for `config` to pass.)

- [ ] **Step 5: Commit**

```bash
git add deployments/app/docker-compose.production.yml \
        deployments/app/docker-compose.staging.yml \
        deployments/app/docker-compose.development.yml
git commit -m "deploy: per-environment compose files (prod/staging/dev)"
```

---

### Task 3: Deploy workflow

Creates `.github/workflows/deploy.yml` with three jobs. Staging deploys on CI-green on main (`:edge`, verified); production on CI-green on a `v*` tag (semver, verified); development on a `deploy:dev`-labeled PR (builds `pr-<n>`, unverified).

**Files:**
- Create: `.github/workflows/deploy.yml`

**Interfaces:**
- Consumes: Task 1's `:edge` / semver tags and cosign signature; Task 2's `docker-compose.<env>.yml` filenames; GitHub Environment secrets `SSH_HOST`/`SSH_USER`/`SSH_KEY`/`HEALTH_URL` and var `SERVER_URL` (created by the operator — Task 5's runbook).

- [ ] **Step 1: Create `.github/workflows/deploy.yml` with this exact content**

```yaml
name: Deploy

on:
  workflow_run:
    workflows: ["CI"]
    types: [completed]
  workflow_dispatch:
    inputs:
      environment:
        description: 'Environment to deploy to'
        required: true
        type: choice
        options:
          - staging
          - production
  pull_request:
    types: [labeled, synchronize]

env:
  REGISTRY: ghcr.io
  IMAGE_NAME: ${{ github.repository }}

jobs:
  deploy-staging:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: read
    if: >-
      (github.event_name == 'workflow_run' &&
        github.event.workflow_run.conclusion == 'success' &&
        github.event.workflow_run.event == 'push' &&
        github.event.workflow_run.head_repository.full_name == github.repository &&
        github.event.workflow_run.head_branch == 'main') ||
      (github.event_name == 'workflow_dispatch' && inputs.environment == 'staging')
    environment:
      name: staging
      url: ${{ vars.SERVER_URL }}
    steps:
      - name: Checkout repository
        uses: actions/checkout@v7
        with:
          ref: ${{ github.event.workflow_run.head_sha || github.sha }}

      - name: Copy docker-compose file
        uses: appleboy/scp-action@v1.0.0
        with:
          host: ${{ secrets.SSH_HOST }}
          username: ${{ secrets.SSH_USER }}
          key: ${{ secrets.SSH_KEY }}
          source: "deployments/app/docker-compose.staging.yml"
          target: "~/mediumrogue-staging"
          strip_components: 2

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v4

      - name: Log into registry ${{ env.REGISTRY }}
        uses: docker/login-action@v4
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Install cosign
        uses: sigstore/cosign-installer@v4.1.2
        with:
          cosign-release: 'v2.2.4'

      - name: Resolve image digest
        id: digest
        env:
          IMAGE_REF: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}:edge
        run: |
          set -euo pipefail
          DIGEST="$(docker buildx imagetools inspect "$IMAGE_REF" --format '{{.Manifest.Digest}}')"
          if [[ ! "$DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]]; then
            echo "ERROR: could not resolve a digest for ${IMAGE_REF} (got '${DIGEST}')" >&2
            exit 1
          fi
          echo "digest=${DIGEST}" >> "$GITHUB_OUTPUT"

      - name: Verify image signature
        env:
          IMAGE_DIGEST_REF: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}@${{ steps.digest.outputs.digest }}
        run: |
          cosign verify "$IMAGE_DIGEST_REF" \
            --certificate-identity-regexp '^https://github\.com/starquake/mediumrogue/\.github/workflows/ci\.yml@refs/heads/main$' \
            --certificate-oidc-issuer 'https://token.actions.githubusercontent.com'

      - name: Deploy to staging server
        uses: appleboy/ssh-action@v1.2.5
        env:
          IMAGE_NAME: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}
          IMAGE_DIGEST: ${{ steps.digest.outputs.digest }}
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GITHUB_ACTOR: ${{ github.actor }}
        with:
          host: ${{ secrets.SSH_HOST }}
          username: ${{ secrets.SSH_USER }}
          key: ${{ secrets.SSH_KEY }}
          envs: IMAGE_NAME,IMAGE_DIGEST,GITHUB_TOKEN,GITHUB_ACTOR
          script: |
            set -e
            mkdir -p ~/mediumrogue-staging
            cd ~/mediumrogue-staging
            umask 077
            cat > .env <<EOF
            IMAGE_NAME='${IMAGE_NAME}'
            IMAGE_DIGEST='${IMAGE_DIGEST}'
            EOF
            mv -f docker-compose.staging.yml docker-compose.yml
            echo "$GITHUB_TOKEN" | docker login ghcr.io -u "$GITHUB_ACTOR" --password-stdin
            docker compose pull
            docker compose up -d
            docker logout ghcr.io

      - name: Verify staging deployment
        run: |
          for i in $(seq 1 12); do
            if curl -fsS "${{ secrets.HEALTH_URL }}" >/dev/null; then
              echo "Health check passed (attempt $i)"
              exit 0
            fi
            echo "Health check attempt $i/12 failed; retrying in 5s..."
            sleep 5
          done
          echo "Health check failed after 12 attempts (60s); failing the deploy" >&2
          exit 1

  deploy-production:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: read
    if: >-
      (github.event_name == 'workflow_run' &&
        github.event.workflow_run.conclusion == 'success' &&
        github.event.workflow_run.event == 'push' &&
        github.event.workflow_run.head_repository.full_name == github.repository &&
        startsWith(github.event.workflow_run.head_branch, 'v')) ||
      (github.event_name == 'workflow_dispatch' && inputs.environment == 'production')
    environment:
      name: production
      url: ${{ vars.SERVER_URL }}
    steps:
      - name: Checkout repository
        uses: actions/checkout@v7
        with:
          ref: ${{ github.event.workflow_run.head_sha || github.sha }}

      - name: Determine image tag
        id: tag
        env:
          IMAGE_TAG: ${{ github.event.workflow_run.head_branch || github.ref_name }}
        run: |
          IMAGE_TAG="${IMAGE_TAG#v}"
          if [[ ! "$IMAGE_TAG" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
            IMAGE_TAG="latest"
          fi
          echo "tag=${IMAGE_TAG}" >> "$GITHUB_OUTPUT"

      - name: Copy docker-compose file
        uses: appleboy/scp-action@v1.0.0
        with:
          host: ${{ secrets.SSH_HOST }}
          username: ${{ secrets.SSH_USER }}
          key: ${{ secrets.SSH_KEY }}
          source: "deployments/app/docker-compose.production.yml"
          target: "~/mediumrogue-production"
          strip_components: 2

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v4

      - name: Log into registry ${{ env.REGISTRY }}
        uses: docker/login-action@v4
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Install cosign
        uses: sigstore/cosign-installer@v4.1.2
        with:
          cosign-release: 'v2.2.4'

      - name: Resolve image digest
        id: digest
        env:
          IMAGE_REF: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}:${{ steps.tag.outputs.tag }}
        run: |
          set -euo pipefail
          DIGEST="$(docker buildx imagetools inspect "$IMAGE_REF" --format '{{.Manifest.Digest}}')"
          if [[ ! "$DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]]; then
            echo "ERROR: could not resolve a digest for ${IMAGE_REF} (got '${DIGEST}')" >&2
            exit 1
          fi
          echo "digest=${DIGEST}" >> "$GITHUB_OUTPUT"

      - name: Verify image signature
        env:
          IMAGE_DIGEST_REF: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}@${{ steps.digest.outputs.digest }}
        run: |
          cosign verify "$IMAGE_DIGEST_REF" \
            --certificate-identity-regexp '^https://github\.com/starquake/mediumrogue/\.github/workflows/ci\.yml@refs/heads/main$' \
            --certificate-oidc-issuer 'https://token.actions.githubusercontent.com'

      - name: Deploy to production server
        uses: appleboy/ssh-action@v1.2.5
        env:
          IMAGE_NAME: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}
          IMAGE_DIGEST: ${{ steps.digest.outputs.digest }}
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GITHUB_ACTOR: ${{ github.actor }}
        with:
          host: ${{ secrets.SSH_HOST }}
          username: ${{ secrets.SSH_USER }}
          key: ${{ secrets.SSH_KEY }}
          envs: IMAGE_NAME,IMAGE_DIGEST,GITHUB_TOKEN,GITHUB_ACTOR
          script: |
            set -e
            mkdir -p ~/mediumrogue-production
            cd ~/mediumrogue-production
            umask 077
            cat > .env <<EOF
            IMAGE_NAME='${IMAGE_NAME}'
            IMAGE_DIGEST='${IMAGE_DIGEST}'
            EOF
            mv -f docker-compose.production.yml docker-compose.yml
            echo "$GITHUB_TOKEN" | docker login ghcr.io -u "$GITHUB_ACTOR" --password-stdin
            docker compose pull
            docker compose up -d
            docker logout ghcr.io

      - name: Verify production deployment
        run: |
          for i in $(seq 1 12); do
            if curl -fsS "${{ secrets.HEALTH_URL }}" >/dev/null; then
              echo "Health check passed (attempt $i)"
              exit 0
            fi
            echo "Health check attempt $i/12 failed; retrying in 5s..."
            sleep 5
          done
          echo "Health check failed after 12 attempts (60s); failing the deploy" >&2
          exit 1

  deploy-development:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
    # Label-only. Fires when a same-repo PR carries the deploy:dev label
    # (on the labeling event or on any later push while it stays labeled).
    if: >-
      github.event_name == 'pull_request' &&
      contains(github.event.pull_request.labels.*.name, 'deploy:dev') &&
      github.event.pull_request.head.repo.full_name == github.repository
    # Single dev slot: a newer labeled push cancels an in-flight dev deploy.
    concurrency:
      group: deploy-development
      cancel-in-progress: true
    environment:
      name: development
      url: ${{ vars.SERVER_URL }}
    steps:
      - name: Checkout PR head
        uses: actions/checkout@v7
        with:
          ref: ${{ github.event.pull_request.head.sha }}

      - name: Copy docker-compose file
        uses: appleboy/scp-action@v1.0.0
        with:
          host: ${{ secrets.SSH_HOST }}
          username: ${{ secrets.SSH_USER }}
          key: ${{ secrets.SSH_KEY }}
          source: "deployments/app/docker-compose.development.yml"
          target: "~/mediumrogue-development"
          strip_components: 2

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v4

      - name: Log into registry ${{ env.REGISTRY }}
        uses: docker/login-action@v4
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      # Build the PR's code and push a pr-<n> tag. No cosign: development is a
      # throwaway sandbox fed only by the operator's own labeled PRs.
      - name: Build and push PR image
        id: build-and-push
        uses: docker/build-push-action@v7
        with:
          context: .
          push: true
          tags: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}:pr-${{ github.event.pull_request.number }}
          cache-from: type=gha
          cache-to: type=gha,mode=max

      - name: Resolve image digest
        id: digest
        env:
          BUILD_DIGEST: ${{ steps.build-and-push.outputs.digest }}
        run: |
          set -euo pipefail
          if [[ ! "$BUILD_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]]; then
            echo "ERROR: build did not return a digest (got '${BUILD_DIGEST}')" >&2
            exit 1
          fi
          echo "digest=${BUILD_DIGEST}" >> "$GITHUB_OUTPUT"

      - name: Deploy to development server
        uses: appleboy/ssh-action@v1.2.5
        env:
          IMAGE_NAME: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}
          IMAGE_DIGEST: ${{ steps.digest.outputs.digest }}
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GITHUB_ACTOR: ${{ github.actor }}
        with:
          host: ${{ secrets.SSH_HOST }}
          username: ${{ secrets.SSH_USER }}
          key: ${{ secrets.SSH_KEY }}
          envs: IMAGE_NAME,IMAGE_DIGEST,GITHUB_TOKEN,GITHUB_ACTOR
          script: |
            set -e
            mkdir -p ~/mediumrogue-development
            cd ~/mediumrogue-development
            umask 077
            cat > .env <<EOF
            IMAGE_NAME='${IMAGE_NAME}'
            IMAGE_DIGEST='${IMAGE_DIGEST}'
            EOF
            mv -f docker-compose.development.yml docker-compose.yml
            echo "$GITHUB_TOKEN" | docker login ghcr.io -u "$GITHUB_ACTOR" --password-stdin
            docker compose pull
            docker compose up -d
            docker logout ghcr.io

      - name: Verify development deployment
        run: |
          for i in $(seq 1 12); do
            if curl -fsS "${{ secrets.HEALTH_URL }}" >/dev/null; then
              echo "Health check passed (attempt $i)"
              exit 0
            fi
            echo "Health check attempt $i/12 failed; retrying in 5s..."
            sleep 5
          done
          echo "Health check failed after 12 attempts (60s); failing the deploy" >&2
          exit 1
```

- [ ] **Step 2: Lint the workflow**

Run: `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/deploy.yml`
Expected: no output, exit 0. (A "shellcheck not found" note is fine.)

- [ ] **Step 3: Cross-check names against Tasks 1 & 2 by eye**

Confirm: staging resolves `:edge`; production resolves `:${{ steps.tag.outputs.tag }}`; development builds `pr-<number>`; all three scp `docker-compose.<env>.yml` and `mv` it to `docker-compose.yml`; the cosign identity regexp exactly matches the Global Constraints value; the dev job is the only one with `packages: write` and no cosign step.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/deploy.yml
git commit -m "deploy: prod/staging/dev deploy workflow (tag/main/labeled-PR)"
```

---

### Task 4: SWAG reverse-proxy confs

Three nginx server blocks, each routing a subdomain to its container. The `/api/events` block disables buffering so SSE turn-bundles flush immediately.

**Files:**
- Create: `deployments/swag/mediumrogue.subdomain.conf`
- Create: `deployments/swag/mediumrogue-staging.subdomain.conf`
- Create: `deployments/swag/mediumrogue-development.subdomain.conf`

**Interfaces:**
- Consumes: the `mediumrogue-<env>` container names from Task 2, reachable by name on SWAG's `web` network.
- Produces: TLS-terminated routing for the three subdomains. The operator (Task 5 runbook) drops these into SWAG's `proxy-confs` dir.

- [ ] **Step 1: Create `deployments/swag/mediumrogue.subdomain.conf`**

```nginx
## mediumrogue — production. Proxies mediumrogue.<domain> to the
## mediumrogue-production container on SWAG's shared `web` network.
## Place in SWAG's proxy-confs/ dir; requires a CNAME for `mediumrogue`.
server {
    listen 443 ssl;
    listen [::]:443 ssl;

    server_name mediumrogue.*;

    include /config/nginx/ssl.conf;

    client_max_body_size 0;

    location / {
        include /config/nginx/proxy.conf;
        include /config/nginx/resolver.conf;
        set $upstream_app mediumrogue-production;
        set $upstream_port 8080;
        set $upstream_proto http;
        proxy_pass $upstream_proto://$upstream_app:$upstream_port;
    }

    # SSE turn-bundle stream. Buffering MUST be off or nginx holds bundles
    # in its response buffer and the client stalls; the long read timeout
    # keeps the idle-heartbeat stream from being reaped mid-turn.
    location /api/events {
        include /config/nginx/proxy.conf;
        include /config/nginx/resolver.conf;
        set $upstream_app mediumrogue-production;
        set $upstream_port 8080;
        set $upstream_proto http;
        proxy_pass $upstream_proto://$upstream_app:$upstream_port;
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 1h;
        proxy_set_header Connection '';
    }
}
```

- [ ] **Step 2: Create `deployments/swag/mediumrogue-staging.subdomain.conf`**

```nginx
## mediumrogue — staging. Proxies mediumrogue-staging.<domain> to the
## mediumrogue-staging container. Requires a CNAME for `mediumrogue-staging`.
server {
    listen 443 ssl;
    listen [::]:443 ssl;

    server_name mediumrogue-staging.*;

    include /config/nginx/ssl.conf;

    client_max_body_size 0;

    location / {
        include /config/nginx/proxy.conf;
        include /config/nginx/resolver.conf;
        set $upstream_app mediumrogue-staging;
        set $upstream_port 8080;
        set $upstream_proto http;
        proxy_pass $upstream_proto://$upstream_app:$upstream_port;
    }

    location /api/events {
        include /config/nginx/proxy.conf;
        include /config/nginx/resolver.conf;
        set $upstream_app mediumrogue-staging;
        set $upstream_port 8080;
        set $upstream_proto http;
        proxy_pass $upstream_proto://$upstream_app:$upstream_port;
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 1h;
        proxy_set_header Connection '';
    }
}
```

- [ ] **Step 3: Create `deployments/swag/mediumrogue-development.subdomain.conf`**

```nginx
## mediumrogue — development. Proxies mediumrogue-development.<domain> to the
## mediumrogue-development container. Requires a CNAME for
## `mediumrogue-development`.
server {
    listen 443 ssl;
    listen [::]:443 ssl;

    server_name mediumrogue-development.*;

    include /config/nginx/ssl.conf;

    client_max_body_size 0;

    location / {
        include /config/nginx/proxy.conf;
        include /config/nginx/resolver.conf;
        set $upstream_app mediumrogue-development;
        set $upstream_port 8080;
        set $upstream_proto http;
        proxy_pass $upstream_proto://$upstream_app:$upstream_port;
    }

    location /api/events {
        include /config/nginx/proxy.conf;
        include /config/nginx/resolver.conf;
        set $upstream_app mediumrogue-development;
        set $upstream_port 8080;
        set $upstream_proto http;
        proxy_pass $upstream_proto://$upstream_app:$upstream_port;
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 1h;
        proxy_set_header Connection '';
    }
}
```

- [ ] **Step 4: Structural check (brace balance + upstream naming)**

Run:
```bash
for f in deployments/swag/mediumrogue*.subdomain.conf; do
  o=$(grep -c '{' "$f"); c=$(grep -c '}' "$f");
  echo "$f: {=$o }=$c"; [ "$o" = "$c" ] || echo "  !! brace mismatch";
done
grep -H 'set $upstream_app' deployments/swag/mediumrogue*.subdomain.conf
```
Expected: each file reports `{=3 }=3`; each file's `upstream_app` matches its environment (`mediumrogue-production` / `-staging` / `-development`). Full `nginx -t` only works on the host, since these `include` SWAG's `/config/nginx/*` files — the operator validates via SWAG reload (Task 5 runbook).

- [ ] **Step 5: Commit**

```bash
git add deployments/swag/mediumrogue.subdomain.conf \
        deployments/swag/mediumrogue-staging.subdomain.conf \
        deployments/swag/mediumrogue-development.subdomain.conf
git commit -m "deploy: SWAG subdomain confs with SSE-safe /api/events routing"
```

---

### Task 5: Operator runbook + docs

The workflow can't create DNS, GitHub Environments, or reload SWAG. This task writes the one-time host/GitHub checklist and the standing docs (FEATURES.md convention + STATUS.md).

**Files:**
- Create: `deployments/README.md`
- Modify: `docs/FEATURES.md` (append a `## Deployment` section)
- Modify: `docs/STATUS.md` (append a short deployment note)

**Interfaces:**
- Consumes: everything from Tasks 1–4 (names, triggers, secrets/vars).
- Produces: the manual setup checklist the operator follows before the first real deploy.

- [ ] **Step 1: Create `deployments/README.md`**

````markdown
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
````

- [ ] **Step 2: Append a `## Deployment` section to `docs/FEATURES.md`**

Add this at the end of `docs/FEATURES.md`:

```markdown
## Deployment

Three environments run from one binary image, on one VPS, behind SWAG. See
`deployments/README.md` for the operator setup checklist.

| Environment  | Domain                                    | Trigger                     | Image           |
| ------------ | ----------------------------------------- | --------------------------- | --------------- |
| production   | `mediumrogue.bananajuice.net`             | push a `v*.*.*` tag         | promoted semver |
| staging      | `mediumrogue-staging.bananajuice.net`     | CI green on `main`          | `:edge`         |
| development  | `mediumrogue-development.bananajuice.net` | `deploy:dev` label on a PR  | `pr-<n>`        |

- **Pipeline:** `ci.yml` builds one image per green `main` commit
  (`sha-<commit>` + `:edge`), cosign-signs it, and (on a `v*` tag) `promote`
  retags it to semver. `deploy.yml` resolves the tag to its digest, verifies
  the signature (staging/production; development skips it), and runs
  `docker compose up -d` over SSH.
- **State:** each environment keeps its own JSON world snapshot on its own
  named volume (`SNAPSHOT_PATH=/data/world.json`); the three worlds are
  independent.
- **No secrets:** the deploy `.env` carries only `IMAGE_NAME`/`IMAGE_DIGEST`.
```

- [ ] **Step 3: Append a deployment note to `docs/STATUS.md`**

Add this note under the current status (near the existing "Deployment itself (VPS/Caddy) remains open" line, or at the end if that's simpler):

```markdown
- **Deployment (landed, 2026-07-12):** three environments — production
  (`mediumrogue.bananajuice.net`, `v*` tag), staging
  (`mediumrogue-staging.bananajuice.net`, main), development
  (`mediumrogue-development.bananajuice.net`, `deploy:dev` PR label) — via
  `.github/workflows/{ci,deploy}.yml`, `deployments/app/*`, and
  `deployments/swag/*`. Image is built once per green main commit, cosign-
  signed, promoted on tag, deployed by digest over SSH behind SWAG. Copies
  topbanana's pipeline minus all secrets. **Operator still owns** DNS CNAMEs,
  GitHub Environments + SSH secrets, and placing the SWAG confs — see
  `deployments/README.md`. First real end-to-end test happens after that
  manual setup.
```

- [ ] **Step 4: Confirm the suite is unaffected**

Run: `make check`
Expected: PASS. No Go/TS source changed, so lint/protocol/typecheck/tests/build are all still green — this confirms the new files didn't accidentally break the build (e.g. a stray file under a compiled path).

- [ ] **Step 5: Commit**

```bash
git add deployments/README.md docs/FEATURES.md docs/STATUS.md
git commit -m "docs: deployment runbook, FEATURES section, STATUS note"
```

---

## Post-implementation: operator handoff (NOT a code task)

After all five tasks land and the PR merges, the deployment is **not live**
until you (the operator) do the `deployments/README.md` one-time setup: DNS
CNAMEs, GitHub Environments + SSH secrets, SWAG confs, GHCR access, and the
`deploy:dev` label. The first `main` push after that publishes the image and
deploys staging; the health-check step in the workflow is the first automated
proof; then tag `v0.0.1` for production and label a PR for development.

## Notes on what is intentionally NOT tested locally

- **Workflow trigger logic** can't be exercised without GitHub — `actionlint`
  + the by-eye cross-checks are the pre-merge gate; the real proof is the
  first live run.
- **cosign sign/verify round-trip** only happens on GitHub's OIDC-enabled
  runners; it cannot be reproduced locally.
- **SWAG conf validity** is confirmed by the host reload, not by local
  `nginx -t` (the confs `include` SWAG's runtime files).
```
