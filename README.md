# go-nfs-client

[![CI](https://github.com/mactav683/go-nfs-client/actions/workflows/ci.yml/badge.svg)](https://github.com/mactav683/go-nfs-client/actions/workflows/ci.yml)
[![Release](https://github.com/mactav683/go-nfs-client/actions/workflows/release.yml/badge.svg)](https://github.com/mactav683/go-nfs-client/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/mactav683/go-nfs-client.svg)](https://pkg.go.dev/github.com/mactav683/go-nfs-client)
[![Go Report Card](https://goreportcard.com/badge/github.com/mactav683/go-nfs-client)](https://goreportcard.com/report/github.com/mactav683/go-nfs-client)

A **pure-Go, userspace NFSv4 client** — no cgo, no kernel mount, no shelling out
to `mount.nfs`. It speaks ONC RPC over **TCP** directly and exposes the export
through Go's standard [`io/fs`](https://pkg.go.dev/io/fs) interfaces plus an
`os.File`-style read-write layer.

- **Zero external dependencies** (standard library only).
- **NFSv4.0 and NFSv4.1** (session/SEQUENCE) with automatic version negotiation.
- **AUTH_SYS / AUTH_NULL** over TCP.
- Synchronous, `context.Context`-aware calls with bounded retry on
  `NFS4ERR_DELAY` / `NFS4ERR_GRACE`.

## Install

```sh
go get github.com/mactav683/go-nfs-client@latest
```

Requires Go 1.24+.

## Usage

### Read-only (`io/fs`)

```go
ctx := context.Background()
cred := rpc.AuthSys{MachineName: "host", UID: 0, GID: 0, GIDs: []uint32{0}}.OpaqueAuth()

cli, err := client.Mount(ctx, "10.0.0.1:2049", "/export", cred)
if err != nil {
    log.Fatal(err)
}
defer cli.Close()

fsys := cli.FS(ctx) // implements fs.FS, fs.StatFS, fs.ReadDirFS

// All the standard io/fs helpers work directly:
data, _ := fs.ReadFile(fsys, "dir/file.txt")
entries, _ := fs.ReadDir(fsys, "dir")
info, _ := fs.Stat(fsys, "dir/file.txt")
_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error { return err })
```

### Read-write (`os.File`-style)

```go
rwfs := cli.RWFS(ctx)
f, _ := rwfs.OpenFile(ctx, "out.bin", os.O_CREATE|os.O_RDWR, 0o644)
_, _ = f.WriteAt([]byte("hello"), 0)
_ = f.Sync()
_ = f.Close()
```

### Selecting the minor version

```go
cli, _ := client.Mount(ctx, addr, export, cred, client.WithMinorVersion(1))   // force v4.1
cli, _ := client.Mount(ctx, addr, export, cred, client.WithAutoMinorVersion()) // negotiate
```

## Packages

| Package  | Purpose                                                              |
|----------|---------------------------------------------------------------------|
| `xdr`    | XDR codec (sticky-error `Encoder`/`Decoder`).                        |
| `rpc`    | ONC RPC over TCP: record marking, AUTH_SYS/AUTH_NULL, `Client.Call`. |
| `nfs4`   | NFSv4 protocol core: COMPOUND engine, ops, v4.1 sessions, retry.     |
| `nfs4/attr` | Attribute bitmap/`fattr4` decode, `Statvfs`.                      |
| `client` | Public surface: `io/fs` read FS + `os.File`-style read-write layer.  |

## Testing

This project uses **Go's built-in tooling** for everything.

### Unit tests + coverage

```sh
go test ./...
go test -race -covermode=atomic -coverprofile=coverage.out ./...
go tool cover -func=coverage.out          # per-function + total summary
go tool cover -html=coverage.out          # open the HTML report
```

### Integration tests (live NFSv4 server)

Integration tests are guarded by `//go:build integration` and need a live
server. A local, reproducible **NFS-Ganesha** server is provided under
[`integration/`](integration/):

```sh
integration/run.sh                        # up + full suite + teardown (v4.0)
NFS_MINORVERSION=1    integration/run.sh   # v4.1
NFS_MINORVERSION=auto integration/run.sh   # negotiate

# Or point at any external server:
NFS_SERVER_ADDR=host:2049 NFS_EXPORT=/path \
  go test -tags integration ./...
```

See [`integration/README.md`](integration/README.md) for details.

## Continuous integration

[`.github/workflows/ci.yml`](.github/workflows/ci.yml) runs on every push/PR:

- **lint** — `gofmt -l` must be clean; `go vet ./...` and
  `go vet -tags integration ./...`.
- **test** — `go build`, then `go test -race -coverprofile`, with a coverage
  summary (`go tool cover -func`) posted to the job summary and the
  `coverage.out`/`.txt`/`.html` uploaded as artifacts.
- **integration** — builds and runs the Dockerized NFS-Ganesha suite across
  NFSv4.0, v4.1, and auto-negotiation.

## Releases

[`.github/workflows/release.yml`](.github/workflows/release.yml) is triggered by
pushing a semver tag. It re-runs the full quality gate (lint + unit + integration)
and then publishes a GitHub Release with auto-generated notes. Pre-1.0 (`v0.*`)
tags are marked as prereleases.

```sh
git tag v0.1.0
git push origin v0.1.0
# CI gates, then the release is published automatically.
```

As a Go library, the released artifact is the tagged module itself — consumers
pull it with `go get github.com/mactav683/go-nfs-client@v0.1.0`.

## Status

Core client is feature-complete and validated against a live server: XDR, RPC,
NFSv4.0 + v4.1, the full `io/fs` read surface, an `os.File`-style write path,
lease management, and DELAY/GRACE retry. Advanced authentication (RPCSEC_GSS /
Kerberos5) is intentionally deferred to keep the build dependency-free; advanced
features (byte-range locking, delegations, ACLs, named attributes) are not yet
implemented.

## License

Apache-2.0. See [LICENSE](LICENSE).
