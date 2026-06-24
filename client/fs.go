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
	"path"
	"strings"

	"github.com/mactav683/go-nfs-client/nfs4"
	"github.com/mactav683/go-nfs-client/nfs4/attr"
)

// Protocol is the subset of NFSv4 read operations the filesystem layer depends
// on. *nfs4.Conn satisfies it; tests provide an in-memory fake.
type Protocol interface {
	RootFH() nfs4.FileHandle
	Lookup(ctx context.Context, dir nfs4.FileHandle, name string) (nfs4.FileHandle, error)
	GetAttr(ctx context.Context, fh nfs4.FileHandle, mask nfs4.Bitmap) (nfs4.Bitmap, []byte, error)
	Read(ctx context.Context, fh nfs4.FileHandle, offset uint64, count uint32) ([]byte, bool, error)
	Readdir(ctx context.Context, dir nfs4.FileHandle, cookie uint64, cookieverf [8]byte, attrMask nfs4.Bitmap) (*nfs4.ReaddirRes, error)
	Readlink(ctx context.Context, fh nfs4.FileHandle) (string, error)
}

// DefaultReadBlockSize is the default per-READ request size.
const DefaultReadBlockSize = 1 << 20 // 1 MiB

// FS is a read-only NFSv4 filesystem implementing io/fs interfaces. The zero
// value is not usable; construct with New.
type FS struct {
	proto Protocol
	ctx   context.Context

	// ReadBlockSize bounds the byte count of each READ request. Reads larger
	// than this are issued as multiple sequential READs (paging). If zero,
	// DefaultReadBlockSize is used.
	ReadBlockSize int
}

// New returns an FS backed by proto, using context.Background for operations.
func New(proto Protocol) *FS {
	return &FS{proto: proto, ctx: context.Background()}
}

// WithContext returns a shallow copy of fsys whose operations use ctx.
func (fsys *FS) WithContext(ctx context.Context) *FS {
	cp := *fsys
	cp.ctx = ctx
	return &cp
}

func (fsys *FS) readBlock() int {
	if fsys.ReadBlockSize > 0 {
		return fsys.ReadBlockSize
	}
	return DefaultReadBlockSize
}

// resolve walks a cleaned io/fs path to its filehandle and decoded attributes.
func (fsys *FS) resolve(name string) (nfs4.FileHandle, *attr.Attributes, error) {
	fh := fsys.proto.RootFH()
	if name != "." {
		for _, comp := range strings.Split(name, "/") {
			next, err := fsys.proto.Lookup(fsys.ctx, fh, comp)
			if err != nil {
				return nil, nil, err
			}
			fh = next
		}
	}
	attrs, err := fsys.stat(fh)
	if err != nil {
		return nil, nil, err
	}
	return fh, attrs, nil
}

// stat fetches and decodes the standard attributes for fh.
func (fsys *FS) stat(fh nfs4.FileHandle) (*attr.Attributes, error) {
	mask, vals, err := fsys.proto.GetAttr(fsys.ctx, fh, nfs4.Bitmap(attr.StandardMask()))
	if err != nil {
		return nil, err
	}
	return attr.Decode(attr.Bitmap(mask), vals)
}

// pathError wraps err in an *fs.PathError for op on name, preserving sentinel
// matching via errors.Is.
func pathError(op, name string, err error) error {
	if err == nil {
		return nil
	}
	return &fs.PathError{Op: op, Path: name, Err: err}
}

// Open implements fs.FS. It returns an *fs.PathError wrapping fs.ErrNotExist for
// missing paths.
func (fsys *FS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	fh, attrs, err := fsys.resolve(name)
	if err != nil {
		return nil, pathError("open", name, err)
	}
	base := path.Base(name)
	if name == "." {
		base = "."
	}
	if attrs.FileMode().IsDir() {
		return &dirFile{fsys: fsys, fh: fh, name: base, attrs: attrs}, nil
	}
	return &file{fsys: fsys, fh: fh, name: base, attrs: attrs}, nil
}

// Stat implements fs.StatFS.
func (fsys *FS) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	_, attrs, err := fsys.resolve(name)
	if err != nil {
		return nil, pathError("stat", name, err)
	}
	base := path.Base(name)
	if name == "." {
		base = "."
	}
	return attrs.FileInfo(base), nil
}

// ReadDir implements fs.ReadDirFS, paging through READDIR until EOF.
func (fsys *FS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	fh, attrs, err := fsys.resolve(name)
	if err != nil {
		return nil, pathError("readdir", name, err)
	}
	if !attrs.FileMode().IsDir() {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: errors.New("not a directory")}
	}
	entries, err := fsys.readDirAll(fh)
	if err != nil {
		return nil, pathError("readdir", name, err)
	}
	return entries, nil
}

// readDirAll pages through all directory entries.
func (fsys *FS) readDirAll(fh nfs4.FileHandle) ([]fs.DirEntry, error) {
	var out []fs.DirEntry
	var cookie uint64
	var cookieverf [8]byte
	mask := nfs4.Bitmap(attr.StandardMask())
	for {
		res, err := fsys.proto.Readdir(fsys.ctx, fh, cookie, cookieverf, mask)
		if err != nil {
			return nil, err
		}
		for _, e := range res.Entries {
			if e.Name == "." || e.Name == ".." {
				cookie = e.Cookie
				continue
			}
			attrs, derr := attr.Decode(attr.Bitmap(e.AttrMask), e.AttrVals)
			if derr != nil {
				return nil, derr
			}
			out = append(out, fs.FileInfoToDirEntry(attrs.FileInfo(e.Name)))
			cookie = e.Cookie
		}
		cookieverf = res.Cookieverf
		if res.EOF {
			break
		}
		if len(res.Entries) == 0 {
			// Defensive: avoid an infinite loop if the server returns no
			// entries without signalling EOF.
			break
		}
	}
	return out, nil
}

// ReadLink returns the target of the symbolic link at name without following
// it. It mirrors the fs.ReadLinkFS-style contract; the standard fs.ReadLinkFS
// interface is adopted once the minimum Go version provides it.
func (fsys *FS) ReadLink(name string) (string, error) {
	if !fs.ValidPath(name) {
		return "", &fs.PathError{Op: "readlink", Path: name, Err: fs.ErrInvalid}
	}
	fh, _, err := fsys.resolve(name)
	if err != nil {
		return "", pathError("readlink", name, err)
	}
	target, err := fsys.proto.Readlink(fsys.ctx, fh)
	if err != nil {
		return "", pathError("readlink", name, err)
	}
	return target, nil
}

// Lstat returns FileInfo for name without following a terminal symbolic link.
func (fsys *FS) Lstat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: fs.ErrInvalid}
	}
	_, attrs, err := fsys.resolve(name)
	if err != nil {
		return nil, pathError("lstat", name, err)
	}
	base := path.Base(name)
	if name == "." {
		base = "."
	}
	return attrs.FileInfo(base), nil
}

// Compile-time interface checks.
var (
	_ fs.FS        = (*FS)(nil)
	_ fs.StatFS    = (*FS)(nil)
	_ fs.ReadDirFS = (*FS)(nil)
)

// file is an open regular file supporting sequential reads.
type file struct {
	fsys   *FS
	fh     nfs4.FileHandle
	name   string
	attrs  *attr.Attributes
	offset uint64
	eof    bool
}

func (f *file) Stat() (fs.FileInfo, error) { return f.attrs.FileInfo(f.name), nil }

func (f *file) Read(p []byte) (int, error) {
	if f.eof {
		return 0, io.EOF
	}
	want := len(p)
	if want > f.fsys.readBlock() {
		want = f.fsys.readBlock()
	}
	data, serverEOF, err := f.fsys.proto.Read(f.fsys.ctx, f.fh, f.offset, uint32(want))
	if err != nil {
		return 0, &fs.PathError{Op: "read", Path: f.name, Err: err}
	}
	n := copy(p, data)
	f.offset += uint64(n)
	if serverEOF && n >= len(data) {
		f.eof = true
	}
	if n == 0 {
		if serverEOF {
			return 0, io.EOF
		}
	}
	return n, nil
}

func (f *file) Close() error { return nil }

// dirFile is an open directory. Reading bytes from it is an error, matching
// os.File semantics; ReadDir streams entries.
type dirFile struct {
	fsys    *FS
	fh      nfs4.FileHandle
	name    string
	attrs   *attr.Attributes
	entries []fs.DirEntry
	read    bool
	pos     int
}

func (d *dirFile) Stat() (fs.FileInfo, error) { return d.attrs.FileInfo(d.name), nil }

func (d *dirFile) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.name, Err: errors.New("is a directory")}
}

func (d *dirFile) Close() error { return nil }

// ReadDir implements fs.ReadDirFile for an opened directory.
func (d *dirFile) ReadDir(n int) ([]fs.DirEntry, error) {
	if !d.read {
		entries, err := d.fsys.readDirAll(d.fh)
		if err != nil {
			return nil, err
		}
		d.entries = entries
		d.read = true
	}
	if n <= 0 {
		rest := d.entries[d.pos:]
		d.pos = len(d.entries)
		return rest, nil
	}
	if d.pos >= len(d.entries) {
		return nil, io.EOF
	}
	end := d.pos + n
	if end > len(d.entries) {
		end = len(d.entries)
	}
	out := d.entries[d.pos:end]
	d.pos = end
	return out, nil
}

var _ fs.ReadDirFile = (*dirFile)(nil)
