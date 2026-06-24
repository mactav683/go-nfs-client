# nfs-client-go

A pure user-space **NFS v3** client for Go. It speaks the NFS wire protocol
directly over TCP (no kernel mount), ported from the Dell/EMC
[`nfs-client-java`](java-impl/) reference implementation.

- **RFC 1813** — NFS Version 3 Protocol
- **RFC 1831** — ONC RPC Version 2
- **RFC 1014** — XDR: External Data Representation
- **RFC 1833** — Binding Protocols for ONC RPC (rpcbind/portmap)

## Status

The v1 bare protocol client and the v2 convenience layer are implemented and
unit-tested. Packages:

| Package | Purpose | RFC |
|---|---|---|
| [`xdr`](xdr) | XDR encode/decode | 1014 |
| [`rpc`](rpc) | ONC RPC v2 messages, AUTH_NONE/AUTH_UNIX, status enums | 1831 |
| [`rpc/transport`](rpc/transport) | TCP transport, record marking, xid demux, retries | 1831 |
| [`portmap`](portmap) | rpcbind GETPORT | 1833 |
| [`mount`](mount) | MOUNT v3 (MNT/UMNT) | 1813 |
| [`nfs3`](nfs3) | NFSv3 types + bare client (all 22 procedures) | 1813 |
| [`nfsfile`](nfsfile) | v2 path-based `File`, chunked `Reader`/`Writer`, symlink loop detection | — |

## Module

```
module github.com/kobitavdish/nfs-client-go
```

Requires Go 1.24+.

## Usage

### v1 — bare protocol client

```go
ctx := context.Background()

// Dials the server, resolves ports via portmap, and mounts the export.
c, err := nfs3.Dial(ctx, "nfs.example.com", "/export/data",
    nfs3.WithUIDGID(0, 0, nil))
if err != nil {
    log.Fatal(err)
}
defer c.Close()

// LOOKUP + READ
fh, attr, err := c.Lookup(ctx, c.Root(), "hello.txt")
if err != nil {
    log.Fatal(err)
}
data, _, err := c.Read(ctx, fh, 0, uint32(attr.Size))
if err != nil {
    log.Fatal(err)
}
fmt.Printf("%s\n", data)

// CREATE + WRITE + COMMIT
nfh, _, _ := c.Create(ctx, c.Root(), "new.txt", nfs3.CreateGuarded,
    nfs3.Sattr3{SetMode: true, Mode: 0o644}, [8]byte{})
c.Write(ctx, nfh, 0, []byte("hi"), nfs3.FileSync)
```

Errors carry the NFS status and satisfy the standard `io/fs` sentinels:

```go
if _, _, err := c.Lookup(ctx, c.Root(), "missing"); errors.Is(err, fs.ErrNotExist) {
    // handle missing file
}
```

### v2 — file abstraction and streams

```go
f := nfsfile.Open(c, "/export/data/big.bin")

r, _ := nfsfile.NewReader(ctx, f)   // chunked to FSINFO rtpref
defer r.Close()
io.Copy(os.Stdout, r)

w, _ := nfsfile.NewWriter(ctx, f, 0) // chunked to wtpref, COMMIT on Close
defer w.Close()
io.Copy(w, src)
```

## Testing

```sh
go test -race ./...                              # unit tests (no network)
```

Integration tests run against a real NFSv3 server and are build-tagged:

```sh
docker compose -f integration/docker-compose.yml up -d
NFS_TEST_SERVER=127.0.0.1 NFS_TEST_EXPORT=/export \
    go test -tags=integration ./integration/...
docker compose -f integration/docker-compose.yml down
```

The build order and design rationale are documented in
[`plans/go-nfsv3-client-plan.md`](plans/go-nfsv3-client-plan.md).

## License

Apache-2.0. See [`LICENSE`](LICENSE).
