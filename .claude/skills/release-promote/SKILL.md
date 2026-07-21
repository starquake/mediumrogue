---
name: release-promote
description: >
  Use when promoting a build through the environments — "ship to staging",
  "promote to prod", "deploy the latest", "cut a release", "put this on dev",
  "redeploy staging", "roll production back". Encodes this repo's dev → staging
  → prod promotion flow on the one VPS behind SWAG: what each hop's trigger is,
  which parts are automatic vs. a deliberate human act, how to verify a deploy
  landed, and the (non-first-class) rollback story. Trigger for any
  ship/promote/deploy request even if the user doesn't say "skill".
---

Promotion is **almost entirely automatic** — the pipeline (`.github/workflows/`
`ci.yml` + `deploy.yml`) does the building, signing, retagging, SSH, and health
check. Your job is the **three human decisions** that trigger a hop, and the
verification after. Do not re-drive the YAML by hand; drive the triggers.

**The three hops** (source of truth: `.github/workflows/deploy.yml`,
`deployments/README.md`, `docs/FEATURES.md#deployment`):

| Env | Public URL | The human act that ships it | Image |
|---|---|---|---|
| development | `mediumrogue-development.bananajuice.net` | add the **`deploy:dev`** label to a PR | `pr-<n>` (unsigned) |
| staging | `mediumrogue-staging.bananajuice.net` | **merge a PR to `main`** | `:edge` (signed) |
| production | `mediumrogue.bananajuice.net` | **push a `v*.*.*` git tag** | promoted semver (signed) |

All three run on one VPS behind the shared SWAG reverse proxy; each has its own
container, its own `mediumrogue_<env>_data` volume, and its own independent
world snapshot. Health endpoint on every env: `GET /healthz`.

## How a hop actually fires (so you know what you're waiting on)

- **staging** — merging to `main` runs **CI**; its `docker-build` job pushes a
  cosign-signed `sha-<commit>` + `:edge` image. `deploy.yml`'s `deploy-staging`
  job fires on the **`workflow_run: CI completed`** event *only* when that CI run
  was a **push to `main` that concluded success**. It resolves `:edge` → digest,
  **cosign-verifies** the signature against `ci.yml@refs/heads/main`, then
  `docker compose up -d` over SSH and polls `/healthz` (12×5s). So: **a green
  merge to main is a staging deploy** — there is no separate staging button to
  press.
- **production** — pushing a `v*.*.*` tag runs CI, but on a tag CI runs **only
  the `promote` job** (lint/test/client/e2e/docker-build all `if:` out on tags —
  the suite already ran on that commit when it was on `main`). `promote` retags
  the existing `sha-<commit>` image to `:X.Y.Z` / `:X.Y` / `:latest` (no
  rebuild, no re-test). `deploy.yml`'s `deploy-production` job then fires on that
  CI `workflow_run` success when `head_branch` starts with `v`, resolves the
  semver tag → digest, cosign-verifies, deploys, health-checks.
- **development** — the `deploy:dev` label on a **same-repo** PR fires
  `deploy-development` directly (a `pull_request: labeled/synchronize` event). It
  **builds the PR head** into a `pr-<n>` image (no cosign — dev is a throwaway
  sandbox), deploys, health-checks. Single dev slot: a newer labeled push
  cancels an in-flight dev deploy (`concurrency: cancel-in-progress`).

## Ship to staging — merge to main

There is no staging-specific step. The **merge gate IS the staging gate**: a PR
only merges carrying `ready to merge` (see the `merge-pr` skill), and that merge
auto-ships to staging on CI green. So:

1. Land the PR the normal way (`merge-pr`).
2. Watch the **CI** run on the merge commit to green — that's what gates the
   deploy; a red main build never reaches `deploy-staging`.
3. Then watch the **Deploy** workflow's `deploy-staging` job. It is a *separate*
   workflow triggered by CI completing — it won't appear on the PR's checks.
   Find it with `gh run list --workflow=Deploy` and watch it to conclusion; a
   failed cosign-verify or a failed `/healthz` poll fails the job loudly.

## Cut a production release — push a `v*.*.*` tag

Production is the one hop that's a **deliberate, separate act** from merging.
The decision to release is the maintainer's; you cut the tag when they say to.

**Preconditions — verify before tagging:**

- The commit you tag must be **on `main` and already built there** — its
  `sha-<commit>` image must exist, because `promote` retags that image and does
  **not** rebuild. Tag a commit that never landed/built on main and `promote`
  fails with `ERROR: …:sha-… not found`. Tag `main`'s HEAD (or an older main
  commit you know built green), never a branch tip.
- The tag must be **strict semver `X.Y.Z`**. A prerelease like `v1.0.0-rc1`
  matches the `v*` deploy guard but is **refused** by `deploy-production`'s
  tag-resolver ("not a strict semver release tag") — it will not deploy.

Cut it:

```bash
git checkout main && git pull --ff-only
git tag v1.2.3            # annotated is fine too: git tag -a v1.2.3 -m "…"
git push origin v1.2.3    # (or: gh release create v1.2.3 --target main)
```

Then watch **CI** (the `promote` job only) go green, then the **Deploy**
workflow's `deploy-production` job. Both are separate runs — enumerate them:

```bash
gh run list --workflow=CI --limit 5
gh run list --workflow=Deploy --limit 5
gh run watch <run-id>          # read it to conclusion; never pipe through tail
```

## Push a PR to dev — the `deploy:dev` label

For previewing an unmerged PR on a real URL:

```bash
gh pr edit <n> --add-label deploy:dev
```

- **The PR branch must contain `deploy.yml`.** `pull_request`-triggered
  workflows run from the *PR branch's* copy of the workflow file, so a branch
  that predates the deploy pipeline won't fire on the label — nothing happens,
  no error. If it's silent, rebase the branch onto `main` and re-apply the
  label.
- Only **same-repo** PRs deploy (the job guards on `head.repo.full_name ==
  repository`); fork PRs are ignored.
- It's a single shared slot — labeling a second PR cancels the first's deploy.
  Remove the label when done; the dev world is safe to wipe anytime.

## Manual redeploy — `workflow_dispatch`

`Actions → Deploy → Run workflow` (or `gh workflow run Deploy -f environment=…`)
offers `staging` / `production`:

- **staging** redeploys the current `:edge` (i.e. latest main).
- **production** redeploys `:latest` — the **newest release tag's** image. It is
  a *redeploy of the current release*, **not** a way to pick an older version.
  There is no dispatch input to target an arbitrary past release.

Development is label-only — no dispatch path.

## Verify a deploy landed

The deploy job already gates itself on `/healthz` (12 attempts × 5s, then fails
the deploy) — a green `deploy-*` job means the health check passed. To confirm
independently:

```bash
curl -fsS https://mediumrogue-staging.bananajuice.net/healthz   # or -production, or the bare prod domain
```

Also: the GitHub **deployment card** (the job's `environment:` shows the env's
`SERVER_URL`), and — if you have SSH access the workflow uses — container logs
on the VPS (`cd ~/mediumrogue-<env> && docker compose logs -f`). Combat/identity
logs carry a category key (`"combat"`, `"identity"`) for grepping.

## Rollback — not first-class; know the two real paths

The infra has **no rollback button**. Options, in preference order:

1. **Roll forward** (usually right): fix on `main`, then cut a new `vX.Y.Z+1`
   tag. This is the clean path and keeps the pipeline's signing/verification
   intact.
2. **Manual pin** (emergency, needs VPS SSH): on the box, edit the env's
   `.env` `IMAGE_DIGEST=` to a known-good older digest and `docker compose up -d`
   in `~/mediumrogue-<env>`. A dispatch-production won't do this for you — it
   only ever deploys `:latest`.

   **Snapshot-version caveat:** snapshots are versioned and the loader
   **rejects, sets aside (`.rejected-<ts>`), and starts fresh** on a version
   mismatch — it never migrates. Rolling the binary *back* across a
   `snapshotVersion` bump means the older binary rejects the newer on-disk world
   and that environment **starts from a fresh world**. Expect it; don't treat
   the empty world as a second failure.

## Out of scope / not automated (say so, don't invent)

- **Environment approval gates.** Each job declares a GitHub `environment:`
  (`production`/`staging`/`development`), so *if* the maintainer has configured
  required-reviewer protection on that environment in repo **Settings →
  Environments**, the deploy job pauses for an in-UI approval. That's a
  GitHub-side setting, **not visible in the repo** — don't assert it exists or
  doesn't; if a deploy sits in "Waiting", that's the gate.
- **Secrets/hosts** (`SSH_HOST`, `SSH_USER`, `SSH_KEY`, `HEALTH_URL`,
  `SERVER_URL`) live in the GitHub Environments, not the repo. The app itself
  has **no** application secrets; the deploy `.env` carries only
  `IMAGE_NAME`/`IMAGE_DIGEST`.
- **One-time infra setup** (DNS CNAMEs, SWAG TLS/proxy-confs, the `web` docker
  network, GHCR pull auth) is `deployments/README.md`'s operator checklist — a
  manual, already-done step, not part of a promotion.
- **Per-environment world knobs** (`TURN_INTERVAL`, `MONSTER_COUNT`, …) are set
  directly in each `deployments/app/docker-compose.<env>.yml`. Retuning one env
  = edit that file and redeploy; it is not carried in the deploy `.env`.
