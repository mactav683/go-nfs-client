// Copyright 2026 The go-nfs-client Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package client

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/mactav683/go-nfs-client/nfs4"
	"github.com/mactav683/go-nfs-client/nfs4/attr"
)

// RWProtocol is the subset of NFSv4 operations the read-write filesystem layer
// depends on. It extends Protocol with the mutating operations. *nfs4.Conn
// satisfies it; tests provide an in-memory fake.
type RWProtocol interface {
	Protocol

	// Open opens (and optionally creates) name within dir. share is an
	// OPEN4_SHARE_ACCESS_* mask. When doCreate is true the file is created with
	// the given create attributes; excl requests GUARDED (exclusive) create.
	Open(ctx context.Context, dir nfs4.FileHandle, name string, share uint32, createAttrMask nfs4.Bitmap, createAttrVals []byte, doCreate, excl bool) (*nfs4.OpenResult, error)
	// Write writes data at offset using the open stateid and stability level,
	// returning bytes written and achieved stability.
	Write(ctx context.Context, fh nfs4.FileHandle, sid nfs4.Stateid, offset uint64, data []byte, stable nfs4.StableHow) (uint32, nfs4.StableHow, error)
	// Commit flushes cached writes in [offset, offset+count) to stable storage.
	Commit(ctx context.Context, fh nfs4.FileHandle, offset uint64, count uint32) error
	// CloseFile closes an open file identified by fh and its open stateid.
	CloseFile(ctx context.Context, fh nfs4.FileHandle, sid nfs4.Stateid) error
	// SetAttr sets attributes on fh using the given stateid (a SIZE change
	// requires an open stateid).
	SetAttr(ctx context.Context, fh nfs4.FileHandle, sid nfs4.Stateid, attrMask nfs4.Bitmap, attrVals []byte) error
}

// RWFS is a read-write NFSv4 filesystem. It embeds the read-only FS behavior
// and adds an os.File-like handle via OpenFile, plus filesystem statistics via
// Statvfs. Construct with NewRW.
type RWFS struct {
	*FS
	proto RWProtocol
}

// NewRW returns an RWFS backed by proto, using context.Background by default.
func NewRW(proto RWProtocol) *RWFS {
	return &RWFS{FS: New(proto), proto: proto}
}

// WithContext returns a shallow copy of fsys whose operations use ctx.
func (fsys *RWFS) WithContext(ctx context.Context) *RWFS {
	return &RWFS{FS: fsys.FS.WithContext(ctx), proto: fsys.proto}
}

// resolveParent splits a cleaned io/fs path into its parent directory
// filehandle and the final path element, looking up the parent.
func (fsys *RWFS) resolveParent(ctx context.Context, name string) (parent nfs4.FileHandle, base string, err error) {
	dir, base := path.Split(name)
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" {
		dir = "."
	}
	parent = fsys.proto.RootFH()
	if dir != "." {
		for _, comp := range strings.Split(dir, "/") {
			next, lerr := fsys.proto.Lookup(ctx, parent, comp)
			if lerr != nil {
				return nil, "", lerr
			}
			parent = next
		}
	}
	return parent, base, nil
}

// shareFor maps an open flag to an OPEN4_SHARE_ACCESS_* mask.
func shareFor(flag int) uint32 {
	switch {
	case flag&os.O_WRONLY != 0:
		return nfs4.OpenShareAccessWrite
	case flag&os.O_RDWR != 0:
		return nfs4.OpenShareAccessBoth
	default: // O_RDONLY (0)
		return nfs4.OpenShareAccessRead
	}
}

// OpenFile opens the named file with the given flags and permission bits,
// returning an os.File-like handle. The flag semantics mirror os.OpenFile:
// O_CREATE creates the file if absent, O_EXCL with O_CREATE fails if it exists,
// O_TRUNC truncates to zero length, and O_APPEND directs writes to end-of-file.
func (fsys *RWFS) OpenFile(ctx context.Context, name string, flag int, perm fs.FileMode) (*File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	parent, base, err := fsys.resolveParent(ctx, name)
	if err != nil {
		return nil, pathError("open", name, err)
	}

	share := shareFor(flag)
	doCreate := flag&os.O_CREATE != 0
	excl := flag&os.O_EXCL != 0

	var createMask nfs4.Bitmap
	var createVals []byte
	if doCreate {
		mode := uint32(perm.Perm())
		m, v := attr.EncodeFattr(attr.SettableAttrs{Mode: &mode})
		createMask = nfs4.Bitmap(m)
		createVals = v
	}

	res, err := fsys.proto.Open(ctx, parent, base, share, createMask, createVals, doCreate, excl)
	if err != nil {
		return nil, pathError("open", name, err)
	}

	f := &File{
		fsys:    fsys,
		ctx:     ctx,
		name:    name,
		fh:      res.FH,
		stateid: res.Stateid,
		flag:    flag,
		share:   share,
	}

	if flag&os.O_TRUNC != 0 && share != nfs4.OpenShareAccessRead {
		if err := f.Truncate(0); err != nil {
			f.Close()
			return nil, pathError("open", name, err)
		}
	}
	return f, nil
}

// File is an os.File-like handle to an open NFSv4 regular file. It is safe for
// sequential use by a single goroutine; concurrent use is guarded for the
// internal offset only and is not intended for parallel I/O on one handle.
type File struct {
	fsys *RWFS
	ctx  context.Context
	name string

	fh      nfs4.FileHandle
	stateid nfs4.Stateid
	flag    int
	share   uint32

	mu     sync.Mutex
	offset int64
	closed bool
}

// ensure File satisfies the common stream interfaces.
var (
	_ io.ReaderAt   = (*File)(nil)
	_ io.WriterAt   = (*File)(nil)
	_ io.ReadWriter = (*File)(nil)
	_ io.Seeker     = (*File)(nil)
	_ io.Closer     = (*File)(nil)
)

func (f *File) errClosed(op string) error {
	return &fs.PathError{Op: op, Path: f.name, Err: fs.ErrClosed}
}

// Name returns the io/fs path the file was opened with.
func (f *File) Name() string { return f.name }

// Stat returns the file's current attributes.
func (f *File) Stat() (fs.FileInfo, error) {
	f.mu.Lock()
	closed := f.closed
	f.mu.Unlock()
	if closed {
		return nil, f.errClosed("stat")
	}
	mask, vals, err := f.fsys.proto.GetAttr(f.ctx, f.fh, nfs4.Bitmap(attr.StandardMask()))
	if err != nil {
		return nil, pathError("stat", f.name, err)
	}
	attrs, err := attr.Decode(attr.Bitmap(mask), vals)
	if err != nil {
		return nil, pathError("stat", f.name, err)
	}
	return attrs.FileInfo(path.Base(f.name)), nil
}

// size fetches the current file size via GETATTR.
func (f *File) size() (int64, error) {
	mask, vals, err := f.fsys.proto.GetAttr(f.ctx, f.fh, nfs4.Bitmap(attr.StandardMask()))
	if err != nil {
		return 0, err
	}
	attrs, err := attr.Decode(attr.Bitmap(mask), vals)
	if err != nil {
		return 0, err
	}
	return int64(attrs.Size), nil
}

// ReadAt reads len(p) bytes from the file at byte offset off. It returns
// io.EOF when fewer than len(p) bytes are available, mirroring io.ReaderAt.
func (f *File) ReadAt(p []byte, off int64) (int, error) {
	f.mu.Lock()
	closed := f.closed
	f.mu.Unlock()
	if closed {
		return 0, f.errClosed("read")
	}
	if off < 0 {
		return 0, &fs.PathError{Op: "read", Path: f.name, Err: fs.ErrInvalid}
	}

	var total int
	for total < len(p) {
		data, eof, err := f.fsys.proto.Read(f.ctx, f.fh, uint64(off)+uint64(total), uint32(len(p)-total))
		if err != nil {
			return total, pathError("read", f.name, err)
		}
		total += copy(p[total:], data)
		if eof || len(data) == 0 {
			return total, io.EOF
		}
	}
	return total, nil
}

// Read reads from the file at the current offset, advancing it.
func (f *File) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, f.errClosed("read")
	}
	if len(p) == 0 {
		return 0, nil
	}
	data, eof, err := f.fsys.proto.Read(f.ctx, f.fh, uint64(f.offset), uint32(len(p)))
	if err != nil {
		return 0, pathError("read", f.name, err)
	}
	n := copy(p, data)
	f.offset += int64(n)
	if n == 0 || (eof && n < len(p)) {
		if n == 0 {
			return 0, io.EOF
		}
	}
	return n, nil
}

// WriteAt writes len(p) bytes to the file at byte offset off, using FILE_SYNC
// stability. It returns a short write error if the server reports fewer bytes
// written than requested.
func (f *File) WriteAt(p []byte, off int64) (int, error) {
	f.mu.Lock()
	closed := f.closed
	f.mu.Unlock()
	if closed {
		return 0, f.errClosed("write")
	}
	if off < 0 {
		return 0, &fs.PathError{Op: "write", Path: f.name, Err: fs.ErrInvalid}
	}
	return f.writeAt(p, off)
}

func (f *File) writeAt(p []byte, off int64) (int, error) {
	var total int
	for total < len(p) {
		n, _, err := f.fsys.proto.Write(f.ctx, f.fh, f.stateid, uint64(off)+uint64(total), p[total:], nfs4.FileSync4)
		if err != nil {
			return total, pathError("write", f.name, err)
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
		total += int(n)
	}
	return total, nil
}

// Write writes to the file at the current offset (or end-of-file when the file
// was opened with O_APPEND), advancing the offset.
func (f *File) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, f.errClosed("write")
	}
	off := f.offset
	if f.flag&os.O_APPEND != 0 {
		sz, err := f.size()
		if err != nil {
			return 0, pathError("write", f.name, err)
		}
		off = sz
	}
	n, err := f.writeAt(p, off)
	f.offset = off + int64(n)
	return n, err
}

// Seek sets the offset for the next Read or Write to offset, interpreted
// according to whence: io.SeekStart, io.SeekCurrent, or io.SeekEnd.
func (f *File) Seek(offset int64, whence int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, f.errClosed("seek")
	}
	var base int64
	switch whence {
	case io.SeekStart:
		base = 0
	case io.SeekCurrent:
		base = f.offset
	case io.SeekEnd:
		sz, err := f.size()
		if err != nil {
			return 0, pathError("seek", f.name, err)
		}
		base = sz
	default:
		return 0, &fs.PathError{Op: "seek", Path: f.name, Err: fs.ErrInvalid}
	}
	abs := base + offset
	if abs < 0 {
		return 0, &fs.PathError{Op: "seek", Path: f.name, Err: fs.ErrInvalid}
	}
	f.offset = abs
	return abs, nil
}

// Truncate changes the size of the file to size bytes. Growing zero-fills.
func (f *File) Truncate(size int64) error {
	f.mu.Lock()
	closed := f.closed
	f.mu.Unlock()
	if closed {
		return f.errClosed("truncate")
	}
	if size < 0 {
		return &fs.PathError{Op: "truncate", Path: f.name, Err: fs.ErrInvalid}
	}
	sz := uint64(size)
	mask, vals := attr.EncodeFattr(attr.SettableAttrs{Size: &sz})
	if err := f.fsys.proto.SetAttr(f.ctx, f.fh, f.stateid, nfs4.Bitmap(mask), vals); err != nil {
		return pathError("truncate", f.name, err)
	}
	return nil
}

// Sync commits the file's data to stable storage on the server.
func (f *File) Sync() error {
	f.mu.Lock()
	closed := f.closed
	f.mu.Unlock()
	if closed {
		return f.errClosed("sync")
	}
	if err := f.fsys.proto.Commit(f.ctx, f.fh, 0, 0); err != nil {
		return pathError("sync", f.name, err)
	}
	return nil
}

// Close closes the open file, releasing the server-side open state.
func (f *File) Close() error {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return f.errClosed("close")
	}
	f.closed = true
	f.mu.Unlock()
	if err := f.fsys.proto.CloseFile(f.ctx, f.fh, f.stateid); err != nil {
		return pathError("close", f.name, err)
	}
	return nil
}

// StatvfsInfo holds filesystem statistics, with byte-count and inode-count
// fields modeled after POSIX statvfs.
type StatvfsInfo struct {
	BlockSize   uint32 // preferred I/O / reporting block size in bytes
	Blocks      uint64 // total blocks in the filesystem
	BlocksFree  uint64 // total free blocks
	BlocksAvail uint64 // free blocks available to non-privileged users
	Files       uint64 // total inodes
	FilesFree   uint64 // total free inodes
	FilesAvail  uint64 // free inodes available to non-privileged users
	MaxName     uint32 // maximum filename length
}

// statvfsBlockSize is the synthetic block size used to convert the server's
// byte-count space attributes into POSIX-style block counts.
const statvfsBlockSize = 4096

// Statvfs returns filesystem statistics for the object named by name (typically
// "." for the export root). Space attributes are reported by the server in
// bytes and converted to blocks using a fixed block size.
func (fsys *RWFS) Statvfs(ctx context.Context, name string) (*StatvfsInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "statvfs", Path: name, Err: fs.ErrInvalid}
	}
	fh := fsys.proto.RootFH()
	if name != "." {
		for _, comp := range strings.Split(name, "/") {
			next, err := fsys.proto.Lookup(ctx, fh, comp)
			if err != nil {
				return nil, pathError("statvfs", name, err)
			}
			fh = next
		}
	}

	mask, vals, err := fsys.proto.GetAttr(ctx, fh, nfs4.Bitmap(attr.StatvfsMask()))
	if err != nil {
		return nil, pathError("statvfs", name, err)
	}
	sv, err := attr.DecodeStatvfs(attr.Bitmap(mask), vals)
	if err != nil {
		return nil, pathError("statvfs", name, err)
	}

	bs := uint64(statvfsBlockSize)
	info := &StatvfsInfo{
		BlockSize:   statvfsBlockSize,
		Blocks:      sv.SpaceTotal / bs,
		BlocksFree:  sv.SpaceFree / bs,
		BlocksAvail: sv.SpaceAvail / bs,
		Files:       sv.FilesTotal,
		FilesFree:   sv.FilesFree,
		FilesAvail:  sv.FilesAvail,
		MaxName:     sv.MaxName,
	}
	return info, nil
}

// errIsExist reports whether err is an "already exists" error, accounting for
// fs.PathError wrapping. Provided so callers can check parity with os.IsExist.
func errIsExist(err error) bool {
	return errors.Is(err, fs.ErrExist)
}

var _ = errIsExist
