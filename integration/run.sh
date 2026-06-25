#!/usr/bin/env bash
# One-command integration run for go-nfs-client.
#
# Brings up the local Dockerized NFS-Ganesha server, runs the //go:build
# integration test suite against it, then tears the server down. Pass extra
# `go test` arguments through, e.g.:
#
#   integration/run.sh                       # full suite, NFSv4.0
#   NFS_MINORVERSION=1 integration/run.sh    # force NFSv4.1
#   NFS_MINORVERSION=auto integration/run.sh # negotiate highest
#   integration/run.sh -run TestRWPathLive -v
#
# Requires Docker (with compose) and Go. No host-side NFS mount is performed.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_FILE="${SCRIPT_DIR}/docker-compose.yml"

# The export is mounted at the NFSv4 pseudo-root, so NFS_EXPORT is "/".
export NFS_SERVER_ADDR="${NFS_SERVER_ADDR:-127.0.0.1:2049}"
export NFS_EXPORT="${NFS_EXPORT:-/}"

cleanup() {
	echo ">> Tearing down NFS server"
	docker compose -f "${COMPOSE_FILE}" down -v >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo ">> Bringing up NFS-Ganesha (this builds the image on first run)"
docker compose -f "${COMPOSE_FILE}" up -d --build --wait

# Seed a pre-existing file into the export, out-of-band (directly in the
# container, not through our client), so TestExistingFilePresent can assert the
# client observes server state it did not create.
export NFS_EXISTING_FILE="${NFS_EXISTING_FILE:-preexisting.txt}"
export NFS_EXISTING_CONTENT="${NFS_EXISTING_CONTENT:-hello from the server before the client connected}"
echo ">> Seeding pre-existing file /export/${NFS_EXISTING_FILE}"
docker exec \
	-e SEED_NAME="${NFS_EXISTING_FILE}" \
	-e SEED_CONTENT="${NFS_EXISTING_CONTENT}" \
	go-nfs-client-ganesha \
	sh -c 'printf "%s" "$SEED_CONTENT" > "/export/$SEED_NAME"'

echo ">> Running integration suite against ${NFS_SERVER_ADDR} (export ${NFS_EXPORT}, minorversion=${NFS_MINORVERSION:-0})"
cd "${REPO_ROOT}"
if [[ $# -gt 0 ]]; then
	go test -tags integration -timeout 300s "$@" ./...
else
	go test -tags integration -timeout 300s ./...
fi

echo ">> Integration suite passed"
