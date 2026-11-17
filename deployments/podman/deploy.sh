# shellcheck shell=sh
# deploy.sh runs on the server, as the environment's own user
# (mediumrogue-<env>, rootless Podman, lingering on), started by deploy-ssh.sh.
# That has copied this script and the environment's unit into ~/$DEPLOY_DIR, a
# folder of the run's own. It reads:
#   DEPLOY_ENV   production, staging or development
#   DEPLOY_DIR   that folder, under the home: mediumrogue-deploy/<id>
#   IMAGE        the image pinned to its digest: ghcr.io/...@sha256:...
#
# It checks the unit with the server's own Quadlet, pulls the image, installs
# the unit as ~/.config/containers/systemd/mediumrogue.container (service
# mediumrogue.service, container mediumrogue), restarts it and waits until the
# app answers /healthz on its port. Anything failing before the restart leaves
# the running app as it was. The world snapshot lives in ~/data/mediumrogue.
# The run's folder is removed in any case. POSIX sh: the login shell is the
# server's.
set -eu
set -f

HEALTH_TRIES="${HEALTH_TRIES:-36}"
HEALTH_SLEEP="${HEALTH_SLEEP:-5}"
UNITS="$HOME/.config/containers/systemd"
DATA="$HOME/data/mediumrogue"
QUADLET="${QUADLET:-/usr/libexec/podman/quadlet}"

die() {
  echo "error: $*" >&2
  exit 1
}

# Only a run's own folder, so the trap below never removes anything else.
case "${DEPLOY_DIR:-}" in
  mediumrogue-deploy/*) ;;
  *) die "DEPLOY_DIR is not mediumrogue-deploy/<id>: '${DEPLOY_DIR:-}'" ;;
esac
case "${DEPLOY_DIR#mediumrogue-deploy/}" in
  "" | *[!A-Za-z0-9-]*) die "DEPLOY_DIR is not mediumrogue-deploy/<id>: '$DEPLOY_DIR'" ;;
esac
SRC="$HOME/$DEPLOY_DIR"
trap 'rm -rf "$SRC"' EXIT

case "${DEPLOY_ENV:-}" in
  production | staging | development) ;;
  *) die "DEPLOY_ENV is not production, staging or development: '${DEPLOY_ENV:-}'" ;;
esac
case "${IMAGE:-}" in
  ghcr.io/*@sha256:*) ;;
  *) die "IMAGE is not ghcr.io/...@sha256:...: '${IMAGE:-}'" ;;
esac
digest="${IMAGE#*@sha256:}"
[ "${#digest}" -eq 64 ] || die "IMAGE has no sha256 digest: '$IMAGE'"
case "$digest${IMAGE%%@*}" in
  *[!a-z0-9./_-]*) die "IMAGE holds an unexpected character: '$IMAGE'" ;;
esac

unit="$DEPLOY_ENV.container"
[ -f "$SRC/$unit" ] || die "$SRC/$unit is missing"

: "${XDG_RUNTIME_DIR:=/run/user/$(id -u)}"
export XDG_RUNTIME_DIR
cd "$HOME"
# One deploy at a time; the workflow's concurrency groups queue them too.
exec 9> .deploy.lock
flock -w 900 9
# A run whose deploy never started leaves its upload; none is a day old.
find "$HOME/mediumrogue-deploy" -mindepth 1 -maxdepth 1 -type d -mmin +1440 -exec rm -rf {} + 2> /dev/null || true

umask 077
tmp="$(mktemp -d "$HOME/.deploy.XXXXXX")"
trap 'rm -rf "$tmp" "$SRC"' EXIT
mkdir "$tmp/units"

# The unit as mediumrogue.container, with the image filled in.
grep -qx 'Image=@IMAGE@' "$SRC/$unit" || die "no Image=@IMAGE@ in $unit"
sed "s|^Image=@IMAGE@\$|Image=$IMAGE|" "$SRC/$unit" > "$tmp/units/mediumrogue.container"
if grep -q '@[A-Z_]*@' "$tmp/units/mediumrogue.container"; then
  die "$unit has a value deploy.sh does not fill in"
fi
port="$(sed -n 's/^PublishPort=127\.0\.0\.1:\([0-9]*\):8080$/\1/p' "$tmp/units/mediumrogue.container")"
[ -n "$port" ] || die "no PublishPort=127.0.0.1:<port>:8080 in $unit"
# The server's Podman must accept the unit before anything else happens.
if ! QUADLET_UNIT_DIRS="$tmp/units" "$QUADLET" -dryrun -user > "$tmp/quadlet.out" 2>&1; then
  cat "$tmp/quadlet.out" >&2
  die "this server's Quadlet rejects the unit; nothing was changed"
fi

# Public on GHCR: pulled without a login. Already here (a redeploy): no pull.
if ! podman image exists "$IMAGE"; then
  podman pull --quiet "$IMAGE" > /dev/null
fi

mkdir -p "$DATA" "$UNITS"
chmod 644 "$tmp/units/mediumrogue.container"
mv -f "$tmp/units/mediumrogue.container" "$UNITS/mediumrogue.container"
systemctl --user daemon-reload
systemctl --user restart mediumrogue.service

i=0
until curl -fsS --max-time 5 "http://127.0.0.1:$port/healthz" > /dev/null 2>&1; do
  i=$((i + 1))
  if [ "$i" -ge "$HEALTH_TRIES" ]; then
    systemctl --user status mediumrogue.service --no-pager --lines=0 >&2 || true
    die "the app is not healthy on 127.0.0.1:$port after $((HEALTH_TRIES * HEALTH_SLEEP))s: journalctl --user -u mediumrogue on the server"
  fi
  sleep "$HEALTH_SLEEP"
done
# Images no unit uses any more: earlier deploys' digests.
podman image prune --force > /dev/null 2>&1 || true
echo "mediumrogue $DEPLOY_ENV is healthy on 127.0.0.1:$port ($IMAGE)."
