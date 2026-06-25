# Integration test harness

A local, reproducible NFSv4 server for exercising the `//go:build integration`
tests in this repo. The server is **NFS-Ganesha** (a userspace NFS server)
running in a container and exporting a directory over **NFSv4 (TCP/2049)**.

Because `go-nfs-client` is a pure-userspace TCP client, **no host-side
`mount.nfs` is ever run** — the host only talks to the container over TCP. This
avoids needing kernel NFS modules or privileged kernel mounts on the host, and
works on Linux and on macOS/Docker Desktop (amd64 and Apple Silicon arm64).

## Quick start (one command)

```sh
integration/run.sh                        # full suite, NFSv4.0
NFS_MINORVERSION=1    integration/run.sh   # force NFSv4.1
NFS_MINORVERSION=auto integration/run.sh   # negotiate the highest version
integration/run.sh -run TestRWPathLive -v  # pass-through go test args
```

`run.sh` builds the image (first run only), starts the server, waits for
TCP/2049 to accept connections, runs the suite, and tears the server down.

## Manual control

```sh
# Start the server (builds on first run; --wait blocks until healthy).
docker compose -f integration/docker-compose.yml up -d --build --wait

# Run the suite. The export is mounted at the NFSv4 pseudo-root, so NFS_EXPORT="/".
NFS_SERVER_ADDR=127.0.0.1:2049 NFS_EXPORT=/ \
  go test -tags integration -timeout 300s ./...

# Tear down (the -v also removes the export volume).
docker compose -f integration/docker-compose.yml down -v
```

## Environment variables

| Variable               | Default                  | Meaning                                              |
|------------------------|--------------------------|-----------------------------------------------------|
| `NFS_SERVER_ADDR`      | `127.0.0.1:2049`         | `host:port` of the NFSv4 server                     |
| `NFS_EXPORT`           | `/` (via run.sh)         | Path to the export beneath the pseudo-root          |
| `NFS_MINORVERSION`     | unset (= v4.0)           | `1` = force v4.1, `auto` = negotiate                 |
| `NFS_EXISTING_FILE`    | `preexisting.txt` (run.sh) | Name of a file seeded out-of-band; drives `TestExistingFilePresent` (skips if unset) |
| `NFS_EXISTING_CONTENT` | seeded string (run.sh)   | Expected content of `NFS_EXISTING_FILE`             |

`run.sh` seeds `NFS_EXISTING_FILE` directly inside the container (not via the
client) before the suite runs, so `TestExistingFilePresent` can assert the
client observes a file it did not create. Against an external server, create the
file yourself and set these two variables (or leave them unset to skip the test).

## Files

- [`docker-compose.yml`](docker-compose.yml) — builds and runs the server,
  maps host `2049 -> container 2049`, mounts the config and an export volume.
- [`ganesha/Dockerfile`](ganesha/Dockerfile) — Ubuntu + `nfs-ganesha` +
  `nfs-ganesha-vfs` (multi-arch).
- [`ganesha/export.conf`](ganesha/export.conf) — NFSv4-only, `FSAL_VFS`,
  AUTH_SYS, `No_Root_Squash`; the export is mounted **at the pseudo-root** (`/`)
  so a client opening paths relative to the mount root lands in the RW export.
- [`ganesha/entrypoint.sh`](ganesha/entrypoint.sh) — starts D-Bus and runs
  `ganesha.nfsd` in the foreground.
- [`run.sh`](run.sh) — up + test + down convenience wrapper.

## Notes / troubleshooting

- The startup log prints a couple of **Kerberos/krb5 and "Cannot register NFS V4
  on UDP" warnings**. These are harmless: the tests use AUTH_SYS over TCP only.
- If a test that walks the whole tree (`TestReadPathLive`'s recursive
  `fs.WalkDir`) is slow against a remote, high-latency server, that is a test
  scope/latency issue, not a client bug. Against this local container it is fast.
- Running against an external server instead of the container: just set
  `NFS_SERVER_ADDR`/`NFS_EXPORT` and skip the compose commands.
