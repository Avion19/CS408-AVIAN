#!/usr/bin/env bash
# Build for the EC2 CPU, install the release, restart Avian, and check health.
set -Eeuo pipefail

cd "$(dirname -- "${BASH_SOURCE[0]}")/.."
: "${EC2_HOST:?Set EC2_HOST to the instance hostname or IP}"
: "${SSH_KEY_PATH:?Set SSH_KEY_PATH to the private SSH key path}"
EC2_USER="${EC2_USER:-ubuntu}"
if [[ ! -r "$SSH_KEY_PATH" ]]; then
  printf 'SSH key is unreadable: %s\n' "$SSH_KEY_PATH" >&2
  exit 1
fi
for tool in ssh scp rsync curl; do
  command -v "$tool" >/dev/null 2>&1 || { printf '%s is required.\n' "$tool" >&2; exit 1; }
done
go_bin=go
if ! command -v go >/dev/null 2>&1; then
  go_bin=./.tools/go/bin/go
  [[ -x "$go_bin" ]] || { printf 'Go is required. Run ./start.sh or install Go first.\n' >&2; exit 1; }
fi

target="${EC2_USER}@${EC2_HOST}"
ssh_options=(-i "$SSH_KEY_PATH" -o BatchMode=yes -o StrictHostKeyChecking=accept-new)
machine="$(ssh "${ssh_options[@]}" "$target" uname -m)"
case "$machine" in
  x86_64|amd64) architecture=amd64 ;;
  aarch64|arm64) architecture=arm64 ;;
  *) printf 'Unsupported EC2 architecture: %s\n' "$machine" >&2; exit 1 ;;
esac

stage="$(mktemp -d /tmp/avian-deploy.XXXXXX)"
trap 'rm -rf "$stage"' EXIT
# The pure-Go SQLite driver allows a cross-compiled Linux binary.
GOOS=linux GOARCH="$architecture" CGO_ENABLED=0 "$go_bin" build -o "$stage/avian" .
ssh "${ssh_options[@]}" "$target" 'mkdir -p ~/avian-stage/static'
scp "${ssh_options[@]}" "$stage/avian" "$target:avian-stage/avian"
printf -v rsync_ssh 'ssh -i %q -o BatchMode=yes -o StrictHostKeyChecking=accept-new' "$SSH_KEY_PATH"
rsync -az -e "$rsync_ssh" static/ "$target:avian-stage/static/"
# /var/lib/avian and /etc/avian are untouched, so updates retain data and secrets.
ssh "${ssh_options[@]}" "$target" 'sudo install -m 0755 ~/avian-stage/avian /opt/avian/avian && sudo cp -a ~/avian-stage/static/. /opt/avian/static/ && sudo systemctl restart avian.service && sudo systemctl is-active --quiet avian.service'
curl --fail --silent --show-error --retry 10 --retry-delay 2 --retry-connrefused "http://${EC2_HOST}/api/health" >/dev/null
printf 'Avian is live at http://%s/\n' "$EC2_HOST"
