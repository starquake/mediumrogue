# Deployments

Three environments of the mediumrogue binary on one server, zoot
(`zoot.linuxeverywhere.link`, Fedora). Each runs as its own Linux user with
rootless Podman, as a systemd service written by a Quadlet unit; Caddy on the
host serves HTTPS and proxies each name to the app's port on `127.0.0.1`.
All automation lives in `.github/workflows/{ci,deploy}.yml`; this file is what
the workflows can't do and how the pieces fit.

| Environment | Domain                                    | Trigger                    | User                      | Port |
| ----------- | ----------------------------------------- | -------------------------- | ------------------------- | ---- |
| production  | `mediumrogue.bananajuice.net`             | push a `v*.*.*` git tag    | `mediumrogue-production`  | 8110 |
| staging     | `mediumrogue-staging.bananajuice.net`     | CI green on `main`         | `mediumrogue-staging`     | 8111 |
| development | `mediumrogue-development.bananajuice.net` | `deploy:dev` label on a PR | `mediumrogue-development` | 8112 |

## The files

`deployments/podman/`:

- `production.container`, `staging.container`, `development.container`: each
  environment's Quadlet unit, with its world knobs (`TURN_INTERVAL`,
  `WORLD_RADIUS`, `MONSTER_COUNT`, …) set right in it. Edit one and redeploy
  to retune that environment. `Image=@IMAGE@` is filled in at deploy time
  with the digest the workflow resolved (and, for staging and production,
  cosign-verified).
- `deploy-ssh.sh` (runs on the GitHub runner): sends `deploy.sh` and the
  environment's unit over SSH into `~/mediumrogue-deploy/<run>` on the server,
  then runs `deploy.sh` there.
- `deploy.sh` (runs on the server, as the environment's user): checks the unit
  with the server's own Quadlet, pulls the image by digest, installs the unit
  as `~/.config/containers/systemd/mediumrogue.container`, restarts
  `mediumrogue.service`, waits for `/healthz` on its port and removes images no
  container uses any more. If anything fails before the restart, the running
  app is left as it was. A lock (`~/.deploy.lock`) keeps two deploys apart.

mediumrogue has no application secrets, so there is no env file.

## On the server

Everything below is set up by Ansible in `starquake/linuxeverywhere-infra`
(`ansible/roles/mediumrogue`, `roles/app_instance`, `hosts/zoot/README.md`);
none of it is done by hand.

- The users `mediumrogue-<env>`: home `0700`, no password, no sudo, lingering
  on (so their services start at boot without a login), subordinate ids for
  rootless Podman. Development's slice is capped (memory, CPU, tasks): it runs
  pull requests' code.
- Each user's `~/.ssh/authorized_keys` holds the public half of that GitHub
  environment's `SSH_KEY`, with `restrict` (commands only: no terminal, no
  forwarding).
- `~/data` is labelled for containers (SELinux). The world snapshot is
  `~/data/mediumrogue/world.json`, owned by the user: the container runs as
  distroless's `nonroot` (65532), mapped to the user with
  `UserNS=keep-id:uid=65532,gid=65532`.
- Caddy has a site per environment in `/etc/caddy/Caddyfile.d/`, proxying to
  `127.0.0.1:<port>` with `flush_interval -1`, so the SSE turn stream
  (`/api/events`) goes out unbuffered. Caddy keeps no access log, so the SSE
  token in that URL's query string is not logged (#200).
- DNS: `*.bananajuice.net` points at zoot.

## GitHub Environments

Three environments, `production`, `staging`, `development`, under repo
Settings → Environments. On each:

- Secrets:
  - `SSH_HOST`: `zoot.linuxeverywhere.link`.
  - `SSH_USER`: `mediumrogue-<env>` (a different user per environment).
  - `SSH_KEY`: that user's deploy key (the private half; the public half is
    in the infra repository's `ansible/roles/mediumrogue/defaults/main.yml`).
  - `HEALTH_URL`: that environment's `https://<domain>/healthz`.
- Variables:
  - `SERVER_URL`: that environment's public URL (shown on the deployment card).

On `development`, restrict deployment branches to `main`: previews run on
`pull_request_target`, which deploys from `main`.

## GHCR package

On the first `main` push, CI publishes `ghcr.io/starquake/mediumrogue`. Keep the
package public: the server pulls anonymously and holds no registry token.

## Development label

Add the `deploy:dev` label to a PR to deploy it to development. Previews run on
`pull_request_target`, so `main`'s copy of `deploy.yml` and of
`deployments/podman/` runs, and the PR's code is used only as the docker build
context. Only same-repo PRs targeting `main` deploy. There is one slot and one
world: the next labelled PR replaces the build and keeps the world, unless its
snapshot version or radius differs (then the old snapshot is set aside and a
fresh world starts).

## Manual redeploy

`Actions → Deploy → Run workflow` offers `staging` (the current `:edge`) or
`production` (the latest release). Development is label-only.

By hand, with the environment's deploy key in `./key`:

```bash
upload="$(mktemp -d)"
cp deployments/podman/deploy.sh deployments/podman/staging.container "$upload/"
SSH_HOST=zoot.linuxeverywhere.link SSH_USER=mediumrogue-staging SSH_KEY="$(cat key)" \
  DEPLOY_ENV=staging IMAGE=ghcr.io/starquake/mediumrogue@sha256:<digest> \
  bash deployments/podman/deploy-ssh.sh "$upload"
```

## On the server, as the environment's user

```bash
systemctl --user status mediumrogue     # running? since when?
journalctl --user -u mediumrogue -f     # the app's logs ("combat"/"identity" keys)
systemctl --user restart mediumrogue    # restart; the world reloads from the snapshot
podman ps                               # the container and its image
```

Logs go to the journal, which bounds their size, so the json-file caps of
#388 are gone with Compose.

**Rollback** (emergency): set `Image=` in
`~/.config/containers/systemd/mediumrogue.container` to a known-good older
digest, then `systemctl --user daemon-reload && systemctl --user restart
mediumrogue`. Rolling back across a `snapshotVersion` bump starts a fresh world
(the newer snapshot is set aside, not lost). Rolling forward with a new tag is
the usual way.
