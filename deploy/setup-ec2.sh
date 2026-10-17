#!/usr/bin/env bash
# One-time Ubuntu EC2 setup: service account, persistent data, systemd, Nginx.
set -Eeuo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
if ((EUID != 0)); then
  printf 'Run with sudo on an Ubuntu EC2 server.\n' >&2
  exit 1
fi
if ! command -v apt-get >/dev/null 2>&1; then
  printf 'This setup supports Ubuntu LTS with apt-get.\n' >&2
  exit 1
fi

apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl nginx rsync
if ! id avian >/dev/null 2>&1; then
  useradd --system --home-dir /var/lib/avian --shell /usr/sbin/nologin avian
fi
install -d -m 0755 -o root -g root /opt/avian /opt/avian/static /etc/avian
install -d -m 0700 -o avian -g avian /var/lib/avian /var/lib/avian/uploads

# Preserve the secret and database configuration on repeated setup runs.
if [[ ! -f /etc/avian/avian.env ]]; then
  umask 077
  secret="$(od -An -tx1 -N32 /dev/urandom | tr -d ' \n')"
  cat > /etc/avian/avian.env <<EOF
LISTEN_ADDR=127.0.0.1:8080
DATABASE_PATH=/var/lib/avian/avian.db
UPLOAD_DIR=/var/lib/avian/uploads
SESSION_SECRET=${secret}
EOF
fi
chmod 0600 /etc/avian/avian.env
chown root:root /etc/avian/avian.env
install -m 0644 "${script_dir}/avian.service" /etc/systemd/system/avian.service

# Remove the prior course demo service when upgrading the existing instance.
if [[ "$(systemctl list-unit-files goth-hello-world.service --no-legend 2>/dev/null)" == goth-hello-world.service* ]]; then
  systemctl disable --now goth-hello-world.service
fi

cat > /etc/nginx/conf.d/avian.conf <<'NGINX'
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
NGINX

rm -f /etc/nginx/sites-enabled/default /etc/nginx/conf.d/default.conf /etc/nginx/conf.d/goth-hello-world.conf
nginx -t
systemctl daemon-reload
systemctl enable avian.service nginx
systemctl restart nginx
printf 'EC2 setup is ready. Deploy Avian from your laptop with deploy/deploy.sh.\n'
