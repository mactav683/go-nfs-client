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
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"testing"
	"time"

	"github.com/mactav683/go-nfs-client/nfs4"
	"github.com/mactav683/go-nfs-client/nfs4/attr"
)

// memNode is an in-memory filesystem node for the fake protocol.
type memNode struct {
	name     string
	ftype    attr.Ftype
	mode     uint32
	data     []byte              // for regular files
	link     string              // for symlinks
	children map[string]*memNode // for directories
	fileid   uint64
}

// memProto is a fake Protocol backed by an in-memory tree, addressing nodes by
// filehandle (the fileid encoded big-endian).
type memProto struct {
	nodes  map[uint64]*memNode
	root   *memNode
	nextID uint64
}

func newMemProto() *memProto {
	mp := &memProto{nodes: map[uint64]*memNode{}, nextID: 1}
	mp.root = mp.add(&memNode{name: "/", ftype: attr.FtypeDir, mode: 0o755, children: map[string]*memNode{}})
	return mp
}

func (mp *memProto) add(n *memNode) *memNode {
	n.fileid = mp.nextID
	mp.nextID++
	mp.nodes[n.fileid] = n
	return n
}

func (mp *memProto) fh(n *memNode) nfs4.FileHandle {
	return nfs4.FileHandle{byte(n.fileid >> 24), byte(n.fileid >> 16), byte(n.fileid >> 8), byte(n.fileid)}
}

func (mp *memProto) node(fh nfs4.FileHandle) *memNode {
	var id uint64
	for _, b := range fh {
		id = id<<8 | uint64(b)
	}
	return mp.nodes[id]
}

func (mp *memProto) mkdir(parent *memNode, name string) *memNode {
	n := mp.add(&memNode{name: name, ftype: attr.FtypeDir, mode: 0o755, children: map[string]*memNode{}})
	parent.children[name] = n
	return n
}

func (mp *memProto) mkfile(parent *memNode, name string, data []byte) *memNode {
	n := mp.add(&memNode{name: name, ftype: attr.FtypeReg, mode: 0o644, data: data})
	parent.children[name] = n
	return n
}

func (mp *memProto) mksymlink(parent *memNode, name, target string) *memNode {
	n := mp.add(&memNode{name: name, ftype: attr.FtypeLnk, mode: 0o777, link: target})
	parent.children[name] = n
	return n
}

// Protocol implementation.

func (mp *memProto) RootFH() nfs4.FileHandle { return mp.fh(mp.root) }

func (mp *memProto) Lookup(ctx context.Context, dir nfs4.FileHandle, name string) (nfs4.FileHandle, error) {
	d := mp.node(dir)
	if d == nil || d.ftype != attr.FtypeDir {
		return nil, &nfs4.StatusError{Status: nfs4.NFS4ERR_NOTDIR}
	}
	child, ok := d.children[name]
	if !ok {
		return nil, &nfs4.StatusError{Status: nfs4.NFS4ERR_NOENT}
	}
	return mp.fh(child), nil
}

func (mp *memProto) encodeAttrs(n *memNode) (nfs4.Bitmap, []byte) {
	mask := attr.StandardMask()
	var buf bytes.Buffer
	enc := newAttrEncoder(&buf)
	for _, num := range mask.SetAttrs() {
		switch num {
		case attr.AttrType:
			enc.u32(uint32(n.ftype))
		case attr.AttrSize:
			enc.u64(uint64(len(n.data)))
		case attr.AttrFSID:
			enc.u64(1)
			enc.u64(0)
		case attr.AttrFileID:
			enc.u64(n.fileid)
		case attr.AttrMode:
			enc.u32(n.mode)
		case attr.AttrNumLinks:
			enc.u32(1)
		case attr.AttrOwner:
			enc.str("0")
		case attr.AttrOwnerGroup:
			enc.str("0")
		case attr.AttrRawDev:
			enc.u32(0)
			enc.u32(0)
		case attr.AttrSpaceUsed:
			enc.u64(uint64(len(n.data)))
		case attr.AttrTimeAccess, attr.AttrTimeMetadata, attr.AttrTimeModify:
			enc.i64(1000)
			enc.u32(0)
		}
	}
	return nfs4.Bitmap(mask), buf.Bytes()
}

func (mp *memProto) GetAttr(ctx context.Context, fh nfs4.FileHandle, mask nfs4.Bitmap) (nfs4.Bitmap, []byte, error) {
	n := mp.node(fh)
	if n == nil {
		return nil, nil, &nfs4.StatusError{Status: nfs4.NFS4ERR_STALE}
	}
	m, v := mp.encodeAttrs(n)
	return m, v, nil
}

func (mp *memProto) Read(ctx context.Context, fh nfs4.FileHandle, offset uint64, count uint32) ([]byte, bool, error) {
	n := mp.node(fh)
	if n == nil || n.ftype != attr.FtypeReg {
		return nil, false, &nfs4.StatusError{Status: nfs4.NFS4ERR_ISDIR}
	}
	if offset >= uint64(len(n.data)) {
		return nil, true, nil
	}
	end := offset + uint64(count)
	if end > uint64(len(n.data)) {
		end = uint64(len(n.data))
	}
	chunk := n.data[offset:end]
	return chunk, end >= uint64(len(n.data)), nil
}

func (mp *memProto) Readdir(ctx context.Context, dir nfs4.FileHandle, cookie uint64, cookieverf [8]byte, attrMask nfs4.Bitmap) (*nfs4.ReaddirRes, error) {
	d := mp.node(dir)
	if d == nil || d.ftype != attr.FtypeDir {
		return nil, &nfs4.StatusError{Status: nfs4.NFS4ERR_NOTDIR}
	}
	// Deterministic order by sorting names.
	names := make([]string, 0, len(d.children))
	for name := range d.children {
		names = append(names, name)
	}
	// simple insertion sort for determinism without importing sort
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	res := &nfs4.ReaddirRes{Status: nfs4.NFS4_OK}
	var idx uint64
	for _, name := range names {
		idx++
		if idx <= cookie {
			continue
		}
		child := d.children[name]
		m, v := mp.encodeAttrs(child)
		res.Entries = append(res.Entries, nfs4.DirEntry{
			Cookie:   idx,
			Name:     name,
			AttrMask: m,
			AttrVals: v,
		})
	}
	res.EOF = true
	return res, nil
}

func (mp *memProto) Readlink(ctx context.Context, fh nfs4.FileHandle) (string, error) {
	n := mp.node(fh)
	if n == nil || n.ftype != attr.FtypeLnk {
		return "", &nfs4.StatusError{Status: nfs4.NFS4ERR_INVAL}
	}
	return n.link, nil
}

// --- Tests ---

func buildTree() *memProto {
	mp := newMemProto()
	mp.mkfile(mp.root, "hello.txt", []byte("hello world"))
	sub := mp.mkdir(mp.root, "sub")
	mp.mkfile(sub, "nested.txt", []byte(bigContent))
	mp.mksymlink(mp.root, "link", "hello.txt")
	return mp
}

// bigContent exceeds a single READ block to exercise paging.
var bigContent = string(bytes.Repeat([]byte("0123456789"), 2000)) // 20000 bytes

func TestOpenReadFile(t *testing.T) {
	fsys := New(buildTree())
	f, err := fsys.Open("hello.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("content = %q", got)
	}
}

func TestOpenMissingReturnsNotExist(t *testing.T) {
	fsys := New(buildTree())
	_, err := fsys.Open("nope.txt")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestReadLargeFilePaging(t *testing.T) {
	fsys := New(buildTree())
	// Force a small read block to exercise multi-READ paging.
	fsys.ReadBlockSize = 4096
	f, err := fsys.Open("sub/nested.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != bigContent {
		t.Fatalf("paged content mismatch: got %d bytes want %d", len(got), len(bigContent))
	}
}

func TestStat(t *testing.T) {
	fsys := New(buildTree())
	fi, err := fs.Stat(fsys, "hello.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if fi.Size() != int64(len("hello world")) {
		t.Fatalf("size = %d", fi.Size())
	}
	if fi.IsDir() {
		t.Fatalf("hello.txt should not be a dir")
	}
}

func TestReadDir(t *testing.T) {
	fsys := New(buildTree())
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	for _, want := range []string{"hello.txt", "sub", "link"} {
		if !names[want] {
			t.Fatalf("missing entry %q in %v", want, names)
		}
	}
}

func TestWalkDir(t *testing.T) {
	fsys := New(buildTree())
	got := map[string]bool{}
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		got[path] = true
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
	for _, want := range []string{".", "hello.txt", "sub", "sub/nested.txt", "link"} {
		if !got[want] {
			t.Fatalf("WalkDir missing %q in %v", want, got)
		}
	}
}

func TestReadLink(t *testing.T) {
	fsys := New(buildTree())
	target, err := fsys.ReadLink("link")
	if err != nil {
		t.Fatalf("ReadLink: %v", err)
	}
	if target != "hello.txt" {
		t.Fatalf("link target = %q", target)
	}
}

// buildTreeNoSymlink builds a tree of only regular files and directories,
// suitable for fstest.TestFS on Go versions whose harness opens every
// discovered entry as a file (symlink Open semantics are exercised separately
// by TestReadLink against buildTree).
func buildTreeNoSymlink() *memProto {
	mp := newMemProto()
	mp.mkfile(mp.root, "hello.txt", []byte("hello world"))
	sub := mp.mkdir(mp.root, "sub")
	mp.mkfile(sub, "nested.txt", []byte(bigContent))
	return mp
}

func TestFstestConformance(t *testing.T) {
	fsys := New(buildTreeNoSymlink())
	// Use a smaller read block to exercise paging within the conformance run.
	fsys.ReadBlockSize = 8192
	if err := fstestTestFS(t, fsys, "hello.txt", "sub/nested.txt", "sub"); err != nil {
		t.Fatalf("fstest.TestFS: %v", err)
	}
}

// helper indirection so we can keep the import local to the test that needs it.
func fstestTestFS(t *testing.T, fsys fs.FS, expected ...string) error {
	t.Helper()
	return testFS(fsys, expected...)
}

var _ = time.Now
