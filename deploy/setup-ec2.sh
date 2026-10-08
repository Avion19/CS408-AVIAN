#!/usr/bin/env bash
set -Eeuo pipefail

APP_NAME="goth-hello-world"
APP_DIR="/opt/${APP_NAME}"
GO_INSTALL_DIR="/usr/local/go"
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

if (( EUID != 0 )); then
	printf 'Run this script on the EC2 server with sudo: sudo bash deploy/setup-ec2.sh\n' >&2
	exit 1
fi
if ! command -v systemctl >/dev/null 2>&1; then
	printf 'Error: this setup requires a systemd-based EC2 image.\n' >&2
	exit 1
fi

APP_USER="${SUDO_USER:-}"
if [[ -z "$APP_USER" || "$APP_USER" == root ]]; then
	for candidate in ubuntu ec2-user; do
		if id "$candidate" >/dev/null 2>&1; then
			APP_USER="$candidate"
			break
		fi
	done
fi
if [[ -z "$APP_USER" ]] || ! id "$APP_USER" >/dev/null 2>&1; then
	printf 'Error: could not identify the EC2 login user. Run this with sudo from the ubuntu or ec2-user account.\n' >&2
	exit 1
fi

if command -v apt-get >/dev/null 2>&1; then
	apt-get update
	DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl nginx rsync tar
elif command -v dnf >/dev/null 2>&1; then
	dnf install -y ca-certificates curl nginx rsync tar
elif command -v yum >/dev/null 2>&1; then
	yum install -y ca-certificates curl nginx rsync tar
else
	printf 'Error: supported package managers are apt-get, dnf, and yum.\n' >&2
	exit 1
fi

case "$(uname -m)" in
	x86_64|amd64) GO_ARCH="amd64" ;;
	aarch64|arm64) GO_ARCH="arm64" ;;
	*) printf 'Error: unsupported EC2 CPU architecture: %s\n' "$(uname -m)" >&2; exit 1 ;;
esac

GO_VERSION="$(curl --fail --silent --show-error --location --retry 3 'https://go.dev/VERSION?m=text' | sed -n '1p')"
if [[ ! "$GO_VERSION" =~ ^go[0-9]+\.[0-9]+(\.[0-9]+)?$ ]]; then
	printf 'Error: Go download service returned an unexpected version: %s\n' "$GO_VERSION" >&2
	exit 1
fi
GO_ARCHIVE="/tmp/${APP_NAME}-${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
curl --fail --show-error --location --retry 3 \
	-o "$GO_ARCHIVE" "https://go.dev/dl/${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
rm -rf "$GO_INSTALL_DIR"
tar -C /usr/local -xzf "$GO_ARCHIVE"
rm -f "$GO_ARCHIVE"

install -d -m 0755 -o "$APP_USER" -g "$APP_USER" "$APP_DIR"
sed "s/__APP_USER__/${APP_USER}/g" "${SCRIPT_DIR}/${APP_NAME}.service" \
	> "/etc/systemd/system/${APP_NAME}.service"
chmod 0644 "/etc/systemd/system/${APP_NAME}.service"

cat > "/etc/nginx/conf.d/${APP_NAME}.conf" <<'NGINX'
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
NGINX

# Remove the packaged catch-all site so this server remains the sole default.
rm -f /etc/nginx/sites-enabled/default /etc/nginx/conf.d/default.conf
nginx -t
systemctl daemon-reload
systemctl enable "${APP_NAME}.service"
systemctl enable nginx
systemctl restart nginx

printf 'EC2 setup is ready for %s. Deploy the repository from your laptop with deploy/deploy.sh.\n' "$APP_NAME"
