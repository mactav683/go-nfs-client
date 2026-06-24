# Go NFS v3 Client — Complete Implementation & Handover Plan

A self-contained specification for porting the Dell/EMC `nfs-client-java`
user-space **NFS v3** client to Go. This is a pure user-space client that speaks
the NFS wire protocol directly over TCP (no kernel mount).

> **How to use this doc**: The Go repo will contain the original Java source under
> `./java-impl`. Throughout this plan, references like
> `java-impl/.../Xdr.java` point to the Java reference implementation you will place there.
> (In the current Java workspace these live at
> `src/main/java/com/emc/ecs/nfsclient/...`.)

## RFCs

- **RFC 1813** — NFS Version 3 Protocol — https://tools.ietf.org/html/rfc1813
- **RFC 1831** — ONC RPC Version 2 — https://tools.ietf.org/html/rfc1831
- **RFC 1014** — XDR: External Data Representation — https://tools.ietf.org/html/rfc1014
- **RFC 1833** — Binding Protocols for ONC RPC (rpcbind/portmap) — https://tools.ietf.org/html/rfc1833

---

## 1. Confirmed design decisions

1. **Scope (middle path)**: Ship **v1 = bare protocol client** first (fully tested,
   self-contained), then **v2 = file/stream convenience layer** built on the v1
   public API, with no changes to v1 internals.
2. **Concurrency = demux**: a single TCP connection with a dedicated reader
   goroutine that demultiplexes replies to per-`xid` channels, enabling concurrent
   in-flight requests. (The Java client uses Netty + per-call synchronous waits;
   the Go version improves on this with idiomatic demux.)
3. **Authentication = match Java**: AUTH_NONE + AUTH_UNIX only. No RPCSEC_GSS/Kerberos.

### Minor decisions (default unless overridden)
- Transport: **TCP only** (Java is TCP-only).
- Privileged ports (<1024): **opt-in** option; replicate the Java auto-escalation
  on auth rejection (`RpcWrapper` retries with a privileged port on AUTH error).
- Large READ/WRITE: **auto-chunk** using server-advertised `rtmax`/`wtmax`/`dtpref`
  from FSINFO (in the v2 stream layer; v1 exposes raw single-call read/write).
- Error mapping: typed `NfsError` carrying the NFS3ERR_* status, that ALSO satisfies
  `errors.Is(err, fs.ErrNotExist)` / `fs.ErrPermission` / `fs.ErrExist` where sensible.
- Module: `github.com/kobitavdish/nfs-client-go`, Go 1.24+, Apache-2.0.

---

## 2. Go module layout

```
nfs-client-go/
  go.mod                      module github.com/kobitavdish/nfs-client-go
  LICENSE                     Apache-2.0
  README.md
  java-impl/                  original Java source (reference only, not built)
  xdr/                        XDR encode/decode (RFC 1014)
    xdr.go
    xdr_test.go
  rpc/                        ONC RPC v2 messages + auth (RFC 1831)
    message.go                CALL/REPLY structs, marshal/unmarshal
    auth.go                   Credential interface, AUTH_NONE, AUTH_UNIX
    status.go                 reply/accept/reject status enums
    error.go                  RpcError
  rpc/transport/              TCP transport (replaces Netty NetMgr/Connection)
    transport.go              Conn: dial, record marking, xid demux, Call()
    recordmarking.go          fragment framing (RFC 1831 record marking)
  portmap/                    rpcbind GETPORT (RFC 1833)
    portmap.go
  mount/                      MOUNT v3 (MNT/UMNT) (RFC 1813)
    mount.go
  nfs3/                       NFSv3 (RFC 1813)
    const.go                  program/proc numbers, status codes, ftype3
    types.go                  fh3, fattr3, sattr3, wcc_data, *_op_attr, nfstime3, specdata3
    client.go                 Nfs3 bare client + options
    procs.go                  the 22 procedures (or split per-proc files)
    error.go                  NfsError (maps to fs.Err*)
  nfsfile/                    (v2) File abstraction + streams
    file.go                   NfsFile/Nfs3File equivalent
    reader.go                 io.Reader stream (NfsFileInputStream)
    writer.go                 io.Writer stream (NfsFileOutputStream)
    linktracker.go            symlink loop detection
  cmd/nfscli/                 (optional) demo CLI
```

### Java → Go package mapping

| Java package/class | Go package | RFC |
|---|---|---|
| `rpc.Xdr` | `xdr` | 1014 |
| `rpc.RpcRequest` / `rpc.RpcResponse` | `rpc` (message.go) | 1831 |
| `rpc.Credential*` (`CredentialNone`, `CredentialUnix`, `CredentialBase`) | `rpc` (auth.go) | 1831 |
| `rpc.ReplyStatus` / `AcceptStatus` / `RejectStatus` / `RpcStatus` | `rpc` (status.go) | 1831 |
| `rpc.RpcException` | `rpc` (error.go) | — |
| `network.RecordMarkingUtil` | `rpc/transport` (recordmarking.go) | 1831 |
| `network.NetMgr` / `Connection` / `ClientIOHandler` / `RPCRecordDecoder` | `rpc/transport` (transport.go) | — |
| `rpc.RpcWrapper` (retries/timeouts/failover) | `rpc/transport` + `nfs3/client.go` | — |
| `portmap.*` (`Portmapper`, `GetPortRequest/Response`) | `portmap` | 1833 |
| `mount.*` (`MountRequest/Response`, `UnmountRequest`, `MountStatus`) | `mount` | 1813 |
| `nfs.*` + `nfs.nfs3.*` (all request/response, enums, attrs) | `nfs3` | 1813 |
| `nfs.io.NfsFile` / `Nfs3File` / `NfsFileBase` | `nfsfile` (v2) | — |
| `NfsFileInputStream` / `NfsFileOutputStream` | `nfsfile` streams (v2) | — |
| `nfs.io.LinkTracker` | `nfsfile` (v2) | — |

---

## 3. The wire format (everything you need to byte-match the server)

### 3.1 XDR primitives (RFC 1014) — ref `java-impl/.../rpc/Xdr.java`

All values are **big-endian**. Every item is padded to a multiple of **4 bytes**.

| Type | Encoding |
|---|---|
| `int32` / `uint32` | 4 bytes, big-endian |
| `int64` / `uint64` (hyper) | 8 bytes, big-endian |
| `bool` | int32: 0 = false, non-zero = true |
| `float32` | int32 of IEEE-754 bits |
| opaque counted (`bytes<>`) | int32 length, then length bytes, then 0–3 pad bytes to 4-byte boundary |
| `string<>` | same as opaque counted (UTF-8 bytes) |
| fixed opaque (`opaque[n]`) | n bytes, then pad to 4-byte boundary, NO length prefix |
| array `T<>` | int32 count, then count encodings of T |

Padding rule (from Java `getBytesOfPadding`):
`pad = (4 - (len % 4)) % 4`.

> **Go design note**: Java used one mutable `Xdr` buffer with cursor + special
> `payload` list for zero-copy READ/WRITE. In Go, prefer two clean types:
> - `xdr.Writer` wrapping a `*bytes.Buffer` (methods: `Uint32`, `Int32`, `Uint64`,
>   `Bool`, `Float32`, `Bytes(b []byte)` for counted, `Fixed(b []byte)` for
>   fixed-opaque, `String(s string)`).
> - `xdr.Reader` wrapping a `[]byte` + offset (mirror getters; return `error` on
>   short buffer instead of panicking).
>
> For zero-copy WRITE payloads, the transport layer accepts an extra
> `payload [][]byte` (or `io.Reader` + length) appended after the XDR header,
> reproducing Java's `putPayloads`. Don't over-engineer a payload list inside the
> XDR type itself.

**Golden-vector tests** (mirror `java-impl/.../rpc/Test_Xdr.java`): round-trip each
primitive; verify exact bytes for known inputs; verify padding for lengths 0,1,2,3,4,5.

### 3.2 ONC RPC v2 (RFC 1831) — ref `java-impl/.../rpc/RpcRequest.java`, `RpcResponse.java`, `CredentialBase.java`

**CALL message body** (what `RpcRequest.marshalling` writes, in order):
```
uint32 xid              // unique per request; Java uses an atomic counter
uint32 msg_type = 0     // CALL
uint32 rpcvers = 2
uint32 prog             // program number (NFS=100003, MOUNT=100005, PORTMAP=100000)
uint32 vers             // program version
uint32 proc             // procedure number
opaque cred             // credential (flavor + body) — see below
opaque verf             // verifier (flavor + body)
... procedure-specific args follow ...
```

**Credential / verifier encoding** (from `CredentialBase.marshalling`):
```
uint32 cred_flavor
opaque cred_body<>      // counted opaque
uint32 verf_flavor
opaque verf_body<>
```
- **AUTH_NONE** (flavor 0): cred_body = empty (`uint32 0` length), verf = AUTH_NONE + empty.
- **AUTH_UNIX** (flavor 1) cred_body (from `CredentialUnix.getCredential`):
  ```
  uint32 stamp           // seconds since epoch
  string machinename<>   // hostname
  uint32 uid
  uint32 gid
  uint32 gids_len
  uint32 gids[gids_len]
  ```
  Verifier for AUTH_UNIX is still AUTH_NONE (flavor 0, empty).

**REPLY message body** (what `RpcResponse.unmarshalling` reads, in order):
```
uint32 xid
uint32 msg_type = 1     // REPLY
uint32 reply_stat       // 0 = MSG_ACCEPTED, 1 = MSG_DENIED
if MSG_ACCEPTED:
    uint32 verf_flavor   // skip
    opaque verf_body<>   // skip (read & discard)
    uint32 accept_stat   // 0 = SUCCESS; others are errors
else: // MSG_DENIED
    uint32 reject_stat
... if accept_stat == SUCCESS, procedure-specific results follow ...
```

Status enums (define in `rpc/status.go`):
- `reply_stat`: MSG_ACCEPTED=0, MSG_DENIED=1
- `accept_stat`: SUCCESS=0, PROG_UNAVAIL=1, PROG_MISMATCH=2, PROC_UNAVAIL=3,
  GARBAGE_ARGS=4, SYSTEM_ERR=5
- `reject_stat`: RPC_MISMATCH=0, AUTH_ERROR=1
- On AUTH_ERROR the Java `RpcWrapper` retries using a **privileged source port**.

### 3.3 Record marking (RFC 1831, over TCP) — ref `java-impl/.../network/RecordMarkingUtil.java`

Each RPC message on a TCP stream is sent as one or more **fragments**:
```
uint32 header  // bit 31 (0x80000000) = last-fragment flag; bits 0..30 = fragment byte length
[fragment bytes]
```
- A full RPC message = concatenation of all fragment payloads until the
  last-fragment flag is set.
- Java caps fragment size at `MTU_SIZE = 1 MiB` and notes some servers reject
  multi-fragment records ("RPC: multiple fragments per record not supported"), so
  it uses a large fragment size to usually send a single fragment.
- **Go decode**: read 4-byte header → mask `& 0x7fffffff` for length, test
  `& 0x80000000` for last → read that many bytes → repeat until last → the
  concatenated bytes are the XDR reply.
- **Go encode**: for v1 simplicity, send the whole message as a **single
  last-fragment** record (header = `len | 0x80000000`, then bytes). Keep the
  1 MiB cap logic available for large WRITE payloads if needed.

### 3.4 Transport with xid demux — replaces `java-impl/.../network/NetMgr.java` & `Connection.java`

Design for `rpc/transport/transport.go`:
```go
type Conn struct {
    netConn net.Conn
    mu      sync.Mutex
    pending map[uint32]chan reply   // xid -> waiter
    ...
}
```
- One **writer path**: `Call(ctx, xid, requestBytes, payload...) (replyBytes, error)`
  registers a channel under `xid`, writes the record-marked frame, then waits on
  the channel or `ctx.Done()`.
- One **reader goroutine** (`readLoop`): continuously reads full record-marked
  messages, parses the leading `xid` (first 4 bytes of the XDR reply), looks up the
  pending channel, and delivers. Unknown/late xids are dropped (log).
- **Timeouts**: derive from `ctx` (Java used a fixed `rpcTimeout` seconds; expose
  via `context.WithTimeout` and/or an option default of 10s).
- **Retries / failover**: replicate `RpcWrapper`:
  - retry up to `maxRetries` (Java default 2 for portmap; NFS has its own) with
    `retryWait` between attempts;
  - on RPC `AUTH_ERROR` reject, retry with a privileged local source port;
  - Java probes multiple resolved IPs (`probeIps`) and rotates on failure — optional
    for v1 (single resolved address is acceptable initially).
- **Privileged source port**: bind the local socket to a port < 1024
  (`net.DialTCP` with a `LocalAddr` whose port is in [1,1023]); requires elevated
  privileges. Make it opt-in.

> Note: Java keeps separate connection pools for privileged vs non-privileged
> ports (`NetMgr._connectionMap` / `_privilegedConnectionMap`). In Go, a single
> `Conn` per (server, privileged?) is sufficient; pool only if you later need it.

### 3.5 PORTMAP / rpcbind GETPORT (RFC 1833) — ref `java-impl/.../portmap/`

- Program **100000**, version **2**, proc **GETPORT = 3**, on TCP **port 111**.
- Credential: AUTH_NONE.
- **GETPORT args** (`GetPortRequest.marshalling`):
  ```
  uint32 prog       // program to look up (NFS=100003 or MOUNT=100005)
  uint32 vers       // version (3)
  uint32 prot       // IPPROTO_TCP = 6 (UDP = 17)
  uint32 port = 0   // ignored for GETPORT
  ```
- **GETPORT result**: `uint32 port` (0 = not registered → error).
- Flow: query MOUNT port and NFS port via portmap before mounting/calling.
  (Some servers register NFS on the well-known 2049; portmap is still the correct
  general approach and matches Java.)

### 3.6 MOUNT v3 (RFC 1813) — ref `java-impl/.../mount/`

- Program **100005**, version **3**.
- **MNT = proc 1** args: `string dirpath<>` (the export path).
- **MNT result** (`MountResponse.unmarshalling`):
  ```
  uint32 mountstat3        // 0 = MNT3_OK
  if MNT3_OK:
      opaque fhandle3<>    // the ROOT file handle (counted opaque, ≤ 64 bytes in v3)
      uint32 auth_count
      uint32 auth_flavors[auth_count]
  ```
- **UMNT = proc 3** args: `string dirpath<>`. No meaningful result.
- `mountstat3` values: MNT3_OK=0, MNT3ERR_PERM=1, MNT3ERR_NOENT=2, MNT3ERR_IO=5,
  MNT3ERR_ACCES=13, MNT3ERR_NOTDIR=20, MNT3ERR_INVAL=22, MNT3ERR_NAMETOOLONG=63,
  MNT3ERR_NOTSUPP=10004, MNT3ERR_SERVERFAULT=10006.

### 3.7 NFSv3 (RFC 1813) — ref `java-impl/.../nfs/` and `.../nfs/nfs3/`

- Program **100003**, version **3**.

**Procedure numbers** (`Nfs.NFSPROC3_*`):
```
NULL=0  GETATTR=1  SETATTR=2  LOOKUP=3  ACCESS=4  READLINK=5  READ=6  WRITE=7
CREATE=8  MKDIR=9  SYMLINK=10  MKNOD=11  REMOVE=12  RMDIR=13  RENAME=14  LINK=15
READDIR=16  READDIRPLUS=17  FSSTAT=18  FSINFO=19  PATHCONF=20  COMMIT=21
```

**Status codes `nfsstat3`** (`NfsStatus`): NFS3_OK=0, NFS3ERR_PERM=1,
NFS3ERR_NOENT=2, NFS3ERR_IO=5, NFS3ERR_NXIO=6, NFS3ERR_ACCES=13, NFS3ERR_EXIST=17,
NFS3ERR_XDEV=18, NFS3ERR_NODEV=19, NFS3ERR_NOTDIR=20, NFS3ERR_ISDIR=21,
NFS3ERR_INVAL=22, NFS3ERR_FBIG=27, NFS3ERR_NOSPC=28, NFS3ERR_ROFS=30,
NFS3ERR_MLINK=31, NFS3ERR_NAMETOOLONG=63, NFS3ERR_NOTEMPTY=66, NFS3ERR_DQUOT=69,
NFS3ERR_STALE=70, NFS3ERR_REMOTE=71, NFS3ERR_BADHANDLE=10001,
NFS3ERR_NOT_SYNC=10002, NFS3ERR_BAD_COOKIE=10003, NFS3ERR_NOTSUPP=10004,
NFS3ERR_TOOSMALL=10005, NFS3ERR_SERVERFAULT=10006, NFS3ERR_BADTYPE=10007,
NFS3ERR_JUKEBOX=10008.
(Confirm exact list against `java-impl/.../nfs/NfsStatus.java` and RFC 1813 §2.6.)

**`ftype3`** (`NfsType`): NF3REG=1, NF3DIR=2, NF3BLK=3, NF3CHR=4, NF3LNK=5,
NF3SOCK=6, NF3FIFO=7.

**`nfstime3`** (`NfsTime`): `uint32 seconds; uint32 nseconds`.

**`specdata3`**: `uint32 specdata1; uint32 specdata2` (rdev major/minor).

**`fattr3`** (`NfsGetAttributes.unmarshalling`, decode in this exact order):
```
uint32 type        // ftype3
uint32 mode
uint32 nlink
uint32 uid
uint32 gid
uint64 size
uint64 used
uint32 rdev.specdata1
uint32 rdev.specdata2
uint64 fsid
uint64 fileid
nfstime3 atime
nfstime3 mtime
nfstime3 ctime
```
> Go: use `uint32`/`uint64` directly (Java widened uint32 to `long` for lack of
> unsigned types — not needed in Go).

**`post_op_attr`**: `bool attributes_follow; if true { fattr3 }`.
**`pre_op_attr`** (`NfsPreOpAttributes`): `bool follows; if true { uint64 size;
nfstime3 mtime; nfstime3 ctime }`.
**`wcc_data`** (`NfsWccData`): `pre_op_attr before; post_op_attr after`.
**`post_op_fh3`**: `bool handle_follows; if true { opaque fhandle3<> }`.

**`sattr3`** (`NfsSetAttributes`) — settable attributes, each guarded by a "set"
boolean:
```
bool set_mode;  if true uint32 mode
bool set_uid;   if true uint32 uid
bool set_gid;   if true uint32 gid
bool set_size;  if true uint64 size
uint32 set_atime; // DONT_CHANGE=0, SET_TO_SERVER_TIME=1, SET_TO_CLIENT_TIME=2
                  // if SET_TO_CLIENT_TIME: nfstime3 atime
uint32 set_mtime; // same enum; if client time: nfstime3 mtime
```
(Confirm against `java-impl/.../nfs/NfsSetAttributes.java`.)

**Common request pattern** (`NfsRequestBase.marshalling`): RPC CALL header, then
`opaque fileHandle<>` (the primary fh3), then proc-specific args.

**Common response pattern** (`NfsResponseBase.unmarshalling`): RPC REPLY header,
then `uint32 status` (nfsstat3). Most ops then return attributes / wcc_data per RFC.
Helpers in Java: `unmarshallingAttributes` (post_op_attr), `unmarshallingFileHandle`
(post_op_fh3).

#### Per-procedure arg/result summaries (decode/encode order; confirm each against `java-impl` + RFC 1813 §3)

- **GETATTR(1)** args: `fh3`. result: status; if OK `fattr3`.
- **SETATTR(2)** args: `fh3`, `sattr3`, `guard: bool; if true nfstime3 ctime`.
  result: status, `wcc_data`.
- **LOOKUP(3)** args: dir `fh3`, `string name`. result: status; if OK
  `fh3 object`, `post_op_attr obj_attr`, `post_op_attr dir_attr`.
- **ACCESS(4)** args: `fh3`, `uint32 access_mask`
  (READ=0x1, LOOKUP=0x2, MODIFY=0x4, EXTEND=0x8, DELETE=0x10, EXECUTE=0x20).
  result: status, `post_op_attr`, if OK `uint32 access`.
- **READLINK(5)** args: `fh3`. result: status, `post_op_attr`, if OK `string path`.
- **READ(6)** args: `fh3`, `uint64 offset`, `uint32 count`. result: status,
  `post_op_attr`; if OK `uint32 count`, `bool eof`, `opaque data<>`.
  (See `NfsReadResponse.unmarshalling`: reads count, eof, then counted data into
  a caller-provided buffer for zero-copy.)
- **WRITE(7)** args: `fh3`, `uint64 offset`, `uint32 count`, `uint32 stable`
  (UNSTABLE=0, DATA_SYNC=1, FILE_SYNC=2), `opaque data<>` (the **payload**).
  result: status, `wcc_data`; if OK `uint32 count`, `uint32 committed`,
  `opaque verf[8]` (writeverf3, fixed 8 bytes). (See `NfsWriteRequest.marshalling`
  + `putPayloads` for zero-copy send.)
- **CREATE(8)** args: dir `fh3`, `string name`, `createhow3`:
  `uint32 mode` (UNCHECKED=0, GUARDED=1, EXCLUSIVE=2); for UNCHECKED/GUARDED then
  `sattr3`; for EXCLUSIVE then `opaque createverf3[8]`. result: status,
  `post_op_fh3`, `post_op_attr`, `wcc_data`. (See `NfsCreateMode`.)
- **MKDIR(9)** args: dir `fh3`, `string name`, `sattr3`. result like CREATE.
- **SYMLINK(10)** args: dir `fh3`, `string name`, `symlinkdata3`:
  `sattr3` + `string linktext`. result like CREATE.
- **MKNOD(11)** args: dir `fh3`, `string name`, `ftype3 type`; for NF3CHR/NF3BLK:
  `sattr3` + `specdata3`; for NF3SOCK/NF3FIFO: `sattr3`. result like CREATE.
- **REMOVE(12)** args: dir `fh3`, `string name`. result: status, `wcc_data`.
- **RMDIR(13)** args: dir `fh3`, `string name`. result: status, `wcc_data`.
- **RENAME(14)** args: from-dir `fh3`, `string from`, to-dir `fh3`, `string to`.
  result: status, `wcc_data fromdir`, `wcc_data todir`.
- **LINK(15)** args: `fh3` (file), dir `fh3`, `string name`. result: status,
  `post_op_attr`, `wcc_data linkdir`.
- **READDIR(16)** args: dir `fh3`, `uint64 cookie`, `opaque cookieverf[8]`,
  `uint32 count`. result: status, `post_op_attr`, if OK `opaque cookieverf[8]`,
  then entry list: repeated `bool value_follows { uint64 fileid; string name;
  uint64 cookie }`, terminated by `bool false`, then `bool eof`.
  (See `NfsDirectoryEntry`.)
- **READDIRPLUS(17)** args: dir `fh3`, `uint64 cookie`, `opaque cookieverf[8]`,
  `uint32 dircount`, `uint32 maxcount`. result: status, `post_op_attr`, if OK
  `opaque cookieverf[8]`, entry list of `{ uint64 fileid; string name;
  uint64 cookie; post_op_attr; post_op_fh3 }`, terminated by `bool false`, then
  `bool eof`. (See `NfsDirectoryPlusEntry`.)
- **FSSTAT(18)** args: `fh3`. result: status, `post_op_attr`, if OK
  `uint64 tbytes, fbytes, abytes, tfiles, ffiles, afiles; uint32 invarsec`.
  (See `NfsFsStat`.)
- **FSINFO(19)** args: `fh3`. result: status, `post_op_attr`, if OK
  `uint32 rtmax, rtpref, rtmult, wtmax, wtpref, wtmult, dtpref;
   uint64 maxfilesize; nfstime3 time_delta; uint32 properties`.
  **`rtpref`/`wtpref`/`dtpref` drive v2 chunk sizing.** (See `NfsFsInfo`.)
- **PATHCONF(20)** args: `fh3`. result: status, `post_op_attr`, if OK
  `uint32 linkmax, name_max; bool no_trunc, chown_restricted, case_insensitive,
   case_preserving`. (See `NfsPathconf*`.)
- **COMMIT(21)** args: `fh3`, `uint64 offset`, `uint32 count`. result: status,
  `wcc_data`, if OK `opaque writeverf3[8]`.

> **Authoritative ordering**: For every struct/proc above, the canonical byte order
> is what the corresponding Java `marshalling`/`unmarshalling` method does — read
> the matching `java-impl` file when implementing each one, and cross-check RFC 1813.

---

## 4. Idiomatic Go API shape (v1 bare client)

```go
package nfs3

type Client struct { /* server, mount path, fh root, transport, creds, opts */ }

func Dial(ctx context.Context, server, exportPath string, opts ...Option) (*Client, error)
func (c *Client) Close() error            // UMNT + close conn

type Option func(*config)
func WithCredential(cred rpc.Credential) Option
func WithUIDGID(uid, gid uint32, gids []uint32) Option
func WithRPCTimeout(d time.Duration) Option
func WithMaxRetries(n int) Option
func WithRetryWait(d time.Duration) Option
func WithPrivilegedPort(enabled bool) Option

// File handle type
type FH []byte

// Procedures (each takes ctx; returns typed result + error)
func (c *Client) Getattr(ctx, fh FH) (Fattr3, error)
func (c *Client) Lookup(ctx, dir FH, name string) (FH, Fattr3, error)
func (c *Client) Access(ctx, fh FH, mask uint32) (uint32, error)
func (c *Client) Readlink(ctx, fh FH) (string, error)
func (c *Client) Read(ctx, fh FH, offset uint64, count uint32) (data []byte, eof bool, err error)
func (c *Client) Write(ctx, fh FH, offset uint64, data []byte, stable Stable) (count uint32, committed Stable, verf [8]byte, err error)
func (c *Client) Create(ctx, dir FH, name string, how CreateHow, attr Sattr3) (FH, Fattr3, error)
func (c *Client) Mkdir(ctx, dir FH, name string, attr Sattr3) (FH, Fattr3, error)
func (c *Client) Symlink(ctx, dir FH, name, target string, attr Sattr3) (FH, Fattr3, error)
func (c *Client) Mknod(...) (...)
func (c *Client) Remove(ctx, dir FH, name string) error
func (c *Client) Rmdir(ctx, dir FH, name string) error
func (c *Client) Rename(ctx, fromDir FH, from string, toDir FH, to string) error
func (c *Client) Link(ctx, fh FH, dir FH, name string) error
func (c *Client) Readdir(ctx, dir FH, cookie uint64, verf [8]byte, count uint32) ([]DirEntry, [8]byte, bool, error)
func (c *Client) ReaddirPlus(...) (...)
func (c *Client) FSStat(ctx, fh FH) (FSStat, error)
func (c *Client) FSInfo(ctx, fh FH) (FSInfo, error)
func (c *Client) Pathconf(ctx, fh FH) (Pathconf, error)
func (c *Client) Commit(ctx, fh FH, offset uint64, count uint32) ([8]byte, error)
func (c *Client) Root() FH                 // root fh from MOUNT
```

**Error type**:
```go
type NfsError struct { Status uint32; Op string }
func (e *NfsError) Error() string
func (e *NfsError) Is(target error) bool   // maps NFS3ERR_NOENT->fs.ErrNotExist,
                                           // NFS3ERR_ACCES/PERM->fs.ErrPermission,
                                           // NFS3ERR_EXIST->fs.ErrExist, etc.
```

---

## 5. v2 convenience layer (after v1 is proven)

- `nfsfile.File`: path-based handle (e.g. `c.Open("/a/b/c")`); resolves by walking
  LOOKUP from root; caches `fattr3` (with invalidation on mutation); methods
  mirroring `java.io.File` (`Exists`, `IsDir`, `Length`, `List`, `Mkdir`, `Delete`,
  `Rename`, `SetAttr`, etc.). Ref `java-impl/.../nfs/io/NfsFile.java`, `Nfs3File.java`.
- `nfsfile.Reader` (`io.Reader`/`io.ReaderAt`/`io.Closer`): chunked READ sized to
  `FSInfo.rtpref`. Ref `NfsFileInputStream`.
- `nfsfile.Writer` (`io.Writer`/`io.Closer`): chunked WRITE sized to
  `FSInfo.wtpref`, with COMMIT on close when using UNSTABLE writes. Ref
  `NfsFileOutputStream`.
- `linktracker.go`: symlink loop detection. Ref `LinkTracker.java`.

---

## 6. Build order (incremental slices — each must compile + pass tests before next)

1. **module + skeleton**: `go.mod`, `LICENSE`, `README.md`, empty packages.
2. **xdr**: `Writer`/`Reader` + golden-vector tests. (Mirror `Test_Xdr.java`.)
3. **rpc**: CALL/REPLY marshal/unmarshal, AUTH_NONE, AUTH_UNIX, status enums,
   `RpcError`; unit tests with golden bytes for a known CALL header.
4. **rpc/transport**: record marking encode/decode (unit tests with crafted
   frames); `Conn` with demux reader goroutine; a loopback test using an in-memory
   pipe / fake server that echoes a known REPLY.
5. **portmap**: GETPORT against the transport (unit test with fake server;
   integration test vs real rpcbind).
6. **mount**: MNT/UMNT (unit test with fake server returning a known root fh;
   integration vs real server).
7. **nfs3 types**: fh3/fattr3/sattr3/wcc_data/post_op_*/nfstime3/specdata3 with
   round-trip unit tests using golden bytes derived from the Java decoders.
8. **nfs3 procs + client**: implement procedures incrementally
   (GETATTR → LOOKUP → READ → WRITE → CREATE/MKDIR → READDIR(PLUS) → rest), each
   with a fake-server unit test and an integration test.
9. **integration harness**: Docker NFSv3 server (Linux kernel server / nfs-ganesha
   / unfsd) in CI; end-to-end mount + full CRUD + read/write + readdir + attrs.
10. **(v2) nfsfile + streams** once v1 is green.

---

## 7. Testing strategy

- **Unit (no network)**:
  - XDR round-trip + exact-byte golden vectors (port `Test_Xdr.java`).
  - RPC header marshal golden bytes; reply parse for ACCEPTED/DENIED/AUTH_ERROR.
  - Record marking: single-fragment and multi-fragment decode.
  - Each NFS struct: decode known byte vectors (capture from the Java decoders or
    from a packet capture) → assert field values.
  - **Fake RPC server** over `net.Pipe()` or a localhost listener that returns
    canned record-marked replies keyed by proc number — lets every proc be tested
    without a real NFS server.
- **Integration (CI, Dockerized NFSv3 export)**:
  - Spin up an NFS v3 server, export a dir, run mount + CRUD + IO + readdir.
  - Mirror the scenarios in `java-impl/.../nfs/nfs3/Test_Nfs3.java`,
    `Test_Nfs3File.java`, `Test_Streams.java`.
- **`go vet`, `staticcheck`, `-race`** on the transport (demux concurrency).

---

## 8. Open issues / risks to watch

1. **Exact XDR ordering of compound results** (wcc_data, post_op_attr placement,
   READDIRPLUS entries, EXCLUSIVE CREATE verifier). Mitigation: implement each by
   reading the matching `java-impl` decoder AND RFC 1813 §3; add golden-byte tests.
2. **Demux correctness under concurrency/cancellation**: ensure a cancelled
   `Call` removes its pending xid entry; reader must not block on a dead waiter.
   Test with `-race` and concurrent calls.
3. **Privileged ports**: requires root/`CAP_NET_BIND_SERVICE`. Keep opt-in; ensure
   the non-privileged path is the default and works against permissive servers.
4. **Short reads/writes**: server may return fewer bytes than requested
   (`count < requested`); the v2 stream layer must loop. v1 returns raw results.
5. **Large WRITE payloads & fragment limits**: respect `wtmax`; for v1 single-call
   write, document that callers must keep `len(data) <= wtmax`. Keep the 1 MiB
   record-fragment behavior from Java available.
6. **Multiple server IPs / failover**: Java rotates resolved IPs. Optional for v1;
   document as a known gap if deferred.
7. **NFS3 status → Go error mapping completeness**: enumerate the full `nfsstat3`
   set from `NfsStatus.java`; decide the `errors.Is` mappings.
8. **Hostname for AUTH_UNIX**: Java uses local hostname; replicate via `os.Hostname()`.
9. **`uint64` size fields**: Go has native `uint64`; do not copy Java's signed-long
   workaround.

---

## 9. Reference index (Java files to consult per Go package)

- `xdr` → `java-impl/.../rpc/Xdr.java`, test `java-impl/.../rpc/Test_Xdr.java`
- `rpc` → `RpcRequest.java`, `RpcResponse.java`, `Credential*.java`,
  `ReplyStatus/AcceptStatus/RejectStatus/RpcStatus.java`, `RpcException.java`
- `rpc/transport` → `network/RecordMarkingUtil.java`, `network/NetMgr.java`,
  `network/Connection.java`, `network/ClientIOHandler.java`,
  `network/RPCRecordDecoder.java`, `rpc/RpcWrapper.java`
- `portmap` → `portmap/Portmapper.java`, `GetPortRequest.java`, `GetPortResponse.java`
- `mount` → `mount/MountRequest.java`, `MountResponse.java`, `UnmountRequest.java`,
  `MountStatus.java`, `MountException.java`
- `nfs3` → `nfs/Nfs.java` (proc numbers), `nfs/NfsStatus.java`, `nfs/NfsType.java`,
  `nfs/NfsTime.java`, `nfs/NfsGetAttributes.java`, `nfs/NfsSetAttributes.java`,
  `nfs/NfsPreOpAttributes.java`, `nfs/NfsWccData.java`, `nfs/NfsRequestBase.java`,
  `nfs/NfsResponseBase.java`, all `nfs/Nfs*Request.java`/`Nfs*Response.java`,
  `nfs/nfs3/Nfs3*Request.java`/`Nfs3*Response.java`, `nfs/nfs3/Nfs3.java` (client),
  `nfs/NfsDirectoryEntry.java`, `nfs/NfsDirectoryPlusEntry.java`,
  `nfs/NfsFsInfo.java`, `nfs/NfsFsStat.java`, `nfs/NfsCreateMode.java`
- `nfsfile` (v2) → `nfs/io/NfsFile.java`, `Nfs3File.java`, `NfsFileBase.java`,
  `NfsFileInputStream.java`, `NfsFileOutputStream.java`, `LinkTracker.java`,
  filters; tests `Test_Nfs3File.java`, `Test_NfsFileBase.java`, `Test_Streams.java`
