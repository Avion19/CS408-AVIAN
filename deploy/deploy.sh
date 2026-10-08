#!/usr/bin/env bash
set -Eeuo pipefail

APP_NAME="goth-hello-world"
REMOTE_DIR="/opt/${APP_NAME}"
EC2_USER="ubuntu"
HOST=""
SSH_KEY=""

usage() {
	cat <<'USAGE'
Usage: ./deploy/deploy.sh -h <PUBLIC-IP-or-hostname> -i <path-to-pem> [-u <ssh-user>]

Deploys this Go app to an EC2 server prepared with deploy/setup-ec2.sh.
USAGE
}

while getopts ':h:i:u:' option; do
	case "$option" in
		h) HOST="$OPTARG" ;;
		i) SSH_KEY="$OPTARG" ;;
		u) EC2_USER="$OPTARG" ;;
		*) usage >&2; exit 2 ;;
	esac
done

if [[ -z "$HOST" || -z "$SSH_KEY" ]]; then
	usage >&2
	exit 2
fi
if [[ ! -r "$SSH_KEY" ]]; then
	printf 'Error: SSH key is missing or unreadable: %s\n' "$SSH_KEY" >&2
	exit 1
fi
for tool in go rsync ssh curl; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		printf 'Error: %s is required to deploy. Install it and run this script again.\n' "$tool" >&2
		exit 1
	fi
done

SSH_TARGET="${EC2_USER}@${HOST}"
SSH_OPTIONS=(-i "$SSH_KEY" -o BatchMode=yes -o StrictHostKeyChecking=accept-new)
printf 'Running project tests before deployment...\n'
go test ./...

printf 'Copying the project to %s:%s...\n' "$SSH_TARGET" "$REMOTE_DIR"
ssh "${SSH_OPTIONS[@]}" "$SSH_TARGET" "mkdir -p '${REMOTE_DIR}'"
printf -v RSYNC_SSH 'ssh -i %q -o BatchMode=yes -o StrictHostKeyChecking=accept-new' "$SSH_KEY"
rsync -az --delete -e "$RSYNC_SSH" \
	--exclude='/.git/' \
	--exclude='.env' \
	--exclude='*.pem' \
	--exclude='/.tools/' \
	--exclude='/.vscode/' \
	--exclude='/goth-hello-world' \
	./ "${SSH_TARGET}:${REMOTE_DIR}/"

printf 'Building the app and restarting its systemd service...\n'
ssh "${SSH_OPTIONS[@]}" "$SSH_TARGET" \
	"cd '${REMOTE_DIR}' && /usr/local/go/bin/go mod download && /usr/local/go/bin/go build -o '${APP_NAME}' . && sudo systemctl restart '${APP_NAME}.service' && sudo systemctl is-active --quiet '${APP_NAME}.service'"

printf 'Checking the deployed app at http://%s/api/health...\n' "$HOST"
curl --fail --silent --show-error --retry 10 --retry-delay 2 --retry-connrefused \
	--max-time 5 "http://${HOST}/api/health" >/dev/null
printf 'Deployment succeeded: http://%s/\n' "$HOST"
