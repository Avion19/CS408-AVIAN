#!/usr/bin/env bash
# Start Avian from a fresh Ubuntu clone without a separate build toolchain.
set -Eeuo pipefail

cd "$(dirname -- "${BASH_SOURCE[0]}")"

GO_VERSION=1.22.2
GO_BIN=go

install_packages() {
  local missing=()
  for tool in curl tar; do
    command -v "$tool" >/dev/null 2>&1 || missing+=("$tool")
  done
  if ((${#missing[@]} == 0)); then return; fi
  if ! command -v apt-get >/dev/null 2>&1; then
    printf 'Missing %s; automatic setup supports Ubuntu with apt-get.\n' "${missing[*]}" >&2
    exit 1
  fi
  local apt=(apt-get)
  if ((EUID != 0)); then apt=(sudo apt-get); fi
  "${apt[@]}" update
  "${apt[@]}" install -y ca-certificates "${missing[@]}"
}

go_is_compatible() {
  command -v "$1" >/dev/null 2>&1 || return 1
  local version major minor
  version="$($1 version)"
  [[ "$version" =~ go([0-9]+)\.([0-9]+) ]] || return 1
  major="${BASH_REMATCH[1]}"
  minor="${BASH_REMATCH[2]}"
  ((major > 1 || (major == 1 && minor >= 22)))
}

# Prefer a suitable system Go; otherwise keep the pinned toolchain in .tools.
if ! go_is_compatible "$GO_BIN"; then
  GO_BIN="./.tools/go/bin/go"
  if ! go_is_compatible "$GO_BIN"; then
    install_packages
    case "$(uname -m)" in
      x86_64|amd64) arch=amd64 ;;
      aarch64|arm64) arch=arm64 ;;
      *) printf 'Unsupported CPU architecture: %s\n' "$(uname -m)" >&2; exit 1 ;;
    esac
    mkdir -p .tools
    archive="$(mktemp /tmp/avian-go.XXXXXX.tar.gz)"
    trap 'rm -f "$archive"' EXIT
    curl --fail --show-error --location --retry 3 -o "$archive" \
      "https://go.dev/dl/go${GO_VERSION}.linux-${arch}.tar.gz"
    rm -rf .tools/go
    tar -C .tools -xzf "$archive"
    rm -f "$archive"
    trap - EXIT
  fi
fi

# Runtime data and the generated secret stay local and private.
umask 077
mkdir -p data/uploads .tools
if [[ ! -f .env ]]; then
  secret="$(od -An -tx1 -N32 /dev/urandom | tr -d ' \n')"
  printf 'SESSION_SECRET=%s\n' "$secret" > .env
fi
set -a
# .env is local project configuration, generated above on first launch.
source .env
set +a
export PORT="${PORT:-8080}"
export DATABASE_PATH="${DATABASE_PATH:-./data/avian.db}"
export UPLOAD_DIR="${UPLOAD_DIR:-./data/uploads}"

# The binary runs migrations and seeds before opening its HTTP listener.
"$GO_BIN" mod download
"$GO_BIN" build -o .tools/avian .
printf 'Avian is starting at http://localhost:%s/\n' "$PORT"
exec ./.tools/avian
