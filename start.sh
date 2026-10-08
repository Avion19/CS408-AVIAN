#!/usr/bin/env bash
set -Eeuo pipefail

if ! command -v go >/dev/null 2>&1; then
	printf 'Error: Go 1.22 or newer is required. Install Go, then run ./start.sh again.\n' >&2
	exit 1
fi

go_version="$(go version | awk '{print $3}')"
if [[ ! "$go_version" =~ ^go([0-9]+)\.([0-9]+) ]]; then
	printf 'Error: could not determine the installed Go version from: %s\n' "$(go version)" >&2
	exit 1
fi
go_major="${BASH_REMATCH[1]}"
go_minor="${BASH_REMATCH[2]}"
if (( go_major < 1 || (go_major == 1 && go_minor < 22) )); then
	printf 'Error: Go 1.22 or newer is required; found %s.\n' "$go_version" >&2
	exit 1
fi

cd "$(dirname -- "${BASH_SOURCE[0]}")"
PORT="${PORT:-8080}"
export PORT
printf 'Downloading Go module dependencies...\n'
go mod download
printf 'Starting the Hello World app at http://localhost:%s\n' "$PORT"
exec go run .
