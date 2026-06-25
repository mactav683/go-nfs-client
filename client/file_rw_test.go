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
	"fmt"
	"io/fs"

	"github.com/mactav683/go-nfs-client/nfs4"
	"github.com/mactav683/go-nfs-client/nfs4/attr"
	"github.com/mactav683/go-nfs-client/xdr"
)

// memRWProto is an in-memory, write-capable fake implementing RWProtocol. It
// models a single flat directory (the export root) of regular files keyed by
// name. It is sufficient to exercise the os.File-like handle and Statvfs
// without a live server.
type memRWProto struct {
	root    nfs4.FileHandle
	nextFH  uint64
	byFH    map[string]*memFile // key: string(FileHandle)
	byName  map[string]*memFile // key: file name within root
	nextSid uint32
}

// memFile is the in-memory state for one regular file.
type memFile struct {
	fh   nfs4.FileHandle
	name string
	mode uint32
	data []byte
}

func newMemRWProto() *memRWProto {
	root := nfs4.FileHandle{'R', 'O', 'O', 'T'}
	return &memRWProto{
		root:   root,
		nextFH: 1,
		byFH:   map[string]*memFile{},
		byName: map[string]*memFile{},
	}
}

func (m *memRWProto) RootFH() nfs4.FileHandle { return m.root }

func (m *memRWProto) newFH() nfs4.FileHandle {
	fh := nfs4.FileHandle(fmt.Appendf(nil, "FH-%d", m.nextFH))
	m.nextFH++
	return fh
}

func (m *memRWProto) Lookup(_ context.Context, dir nfs4.FileHandle, name string) (nfs4.FileHandle, error) {
	if !bytes.Equal(dir, m.root) {
		return nil, nfs4.NFS4ERR_NOTDIR.Err()
	}
	f, ok := m.byName[name]
	if !ok {
		return nil, nfs4.NFS4ERR_NOENT.Err()
	}
	return f.fh, nil
}

func (m *memRWProto) file(fh nfs4.FileHandle) (*memFile, error) {
	f, ok := m.byFH[string(fh)]
	if !ok {
		return nil, nfs4.NFS4ERR_STALE.Err()
	}
	return f, nil
}

// GetAttr encodes either the standard attribute set or the statvfs set,
// selected by which bits the requested mask carries.
func (m *memRWProto) GetAttr(_ context.Context, fh nfs4.FileHandle, mask nfs4.Bitmap) (nfs4.Bitmap, []byte, error) {
	bm := attr.Bitmap(mask)
	if bm.Has(attr.AttrSpaceTotal) || bm.Has(attr.AttrFilesTotal) {
		return m.encodeStatvfs(bm)
	}
	return m.encodeStandard(fh, bm)
}

func (m *memRWProto) encodeStandard(fh nfs4.FileHandle, mask attr.Bitmap) (nfs4.Bitmap, []byte, error) {
	isRoot := bytes.Equal(fh, m.root)
	var f *memFile
	if !isRoot {
		ff, err := m.file(fh)
		if err != nil {
			return nil, nil, err
		}
		f = ff
	}

	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	for _, num := range mask.SetAttrs() {
		switch num {
		case attr.AttrType:
			if isRoot {
				e.Uint32(uint32(attr.FtypeDir))
			} else {
				e.Uint32(uint32(attr.FtypeReg))
			}
		case attr.AttrSize:
			if isRoot {
				e.Uint64(4096)
			} else {
				e.Uint64(uint64(len(f.data)))
			}
		case attr.AttrFSID:
			e.Uint64(1)
			e.Uint64(0)
		case attr.AttrFileID:
			e.Uint64(1)
		case attr.AttrMode:
			if isRoot {
				e.Uint32(0o755)
			} else {
				e.Uint32(f.mode)
			}
		case attr.AttrNumLinks:
			e.Uint32(1)
		case attr.AttrOwner:
			e.String("0")
		case attr.AttrOwnerGroup:
			e.String("0")
		case attr.AttrRawDev:
			e.Uint32(0)
			e.Uint32(0)
		case attr.AttrSpaceUsed:
			if isRoot {
				e.Uint64(4096)
			} else {
				e.Uint64(uint64(len(f.data)))
			}
		case attr.AttrTimeAccess, attr.AttrTimeMetadata, attr.AttrTimeModify:
			e.Int64(0)
			e.Uint32(0)
		default:
			return nil, nil, fmt.Errorf("memRWProto: unsupported standard attr %d", num)
		}
	}
	return nfs4.Bitmap(mask), buf.Bytes(), nil
}

func (m *memRWProto) encodeStatvfs(mask attr.Bitmap) (nfs4.Bitmap, []byte, error) {
	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	for _, num := range mask.SetAttrs() {
		switch num {
		case attr.AttrFSID:
			e.Uint64(1)
			e.Uint64(0)
		case attr.AttrFilesAvail:
			e.Uint64(900)
		case attr.AttrFilesFree:
			e.Uint64(950)
		case attr.AttrFilesTotal:
			e.Uint64(1000)
		case attr.AttrMaxName:
			e.Uint32(255)
		case attr.AttrSpaceAvail:
			e.Uint64(8 << 30)
		case attr.AttrSpaceFree:
			e.Uint64(9 << 30)
		case attr.AttrSpaceTotal:
			e.Uint64(10 << 30)
		default:
			return nil, nil, fmt.Errorf("memRWProto: unsupported statvfs attr %d", num)
		}
	}
	return nfs4.Bitmap(mask), buf.Bytes(), nil
}

func (m *memRWProto) Read(_ context.Context, fh nfs4.FileHandle, offset uint64, count uint32) ([]byte, bool, error) {
	f, err := m.file(fh)
	if err != nil {
		return nil, false, err
	}
	if offset >= uint64(len(f.data)) {
		return nil, true, nil
	}
	end := offset + uint64(count)
	if end > uint64(len(f.data)) {
		end = uint64(len(f.data))
	}
	chunk := append([]byte(nil), f.data[offset:end]...)
	return chunk, end >= uint64(len(f.data)), nil
}

func (m *memRWProto) Readdir(_ context.Context, _ nfs4.FileHandle, _ uint64, _ [8]byte, _ nfs4.Bitmap) (*nfs4.ReaddirRes, error) {
	return &nfs4.ReaddirRes{EOF: true}, nil
}

func (m *memRWProto) Readlink(_ context.Context, _ nfs4.FileHandle) (string, error) {
	return "", nfs4.NFS4ERR_INVAL.Err()
}

func (m *memRWProto) Open(_ context.Context, dir nfs4.FileHandle, name string, _ uint32, _ nfs4.Bitmap, _ []byte, doCreate, excl bool) (*nfs4.OpenResult, error) {
	if !bytes.Equal(dir, m.root) {
		return nil, nfs4.NFS4ERR_NOTDIR.Err()
	}
	f, exists := m.byName[name]
	if exists {
		if doCreate && excl {
			return nil, nfs4.NFS4ERR_EXIST.Err()
		}
	} else {
		if !doCreate {
			return nil, nfs4.NFS4ERR_NOENT.Err()
		}
		f = &memFile{fh: m.newFH(), name: name, mode: 0o644}
		m.byFH[string(f.fh)] = f
		m.byName[name] = f
	}
	m.nextSid++
	sid := nfs4.Stateid{Seqid: m.nextSid}
	return &nfs4.OpenResult{FH: f.fh, Stateid: sid}, nil
}

func (m *memRWProto) Write(_ context.Context, fh nfs4.FileHandle, _ nfs4.Stateid, offset uint64, data []byte, _ nfs4.StableHow) (uint32, nfs4.StableHow, error) {
	f, err := m.file(fh)
	if err != nil {
		return 0, 0, err
	}
	end := offset + uint64(len(data))
	if end > uint64(len(f.data)) {
		grown := make([]byte, end)
		copy(grown, f.data)
		f.data = grown
	}
	copy(f.data[offset:end], data)
	return uint32(len(data)), nfs4.FileSync4, nil
}

func (m *memRWProto) Commit(_ context.Context, fh nfs4.FileHandle, _ uint64, _ uint32) error {
	_, err := m.file(fh)
	return err
}

func (m *memRWProto) CloseFile(_ context.Context, fh nfs4.FileHandle, _ nfs4.Stateid) error {
	_, err := m.file(fh)
	return err
}

// SetAttr supports SIZE (truncate, growing zero-fills) and MODE changes.
func (m *memRWProto) SetAttr(_ context.Context, fh nfs4.FileHandle, _ nfs4.Stateid, attrMask nfs4.Bitmap, attrVals []byte) error {
	f, err := m.file(fh)
	if err != nil {
		return err
	}
	bm := attr.Bitmap(attrMask)
	d := xdr.NewDecoder(bytes.NewReader(attrVals))
	for _, num := range bm.SetAttrs() {
		switch num {
		case attr.AttrSize:
			size := int(d.Uint64())
			if size <= len(f.data) {
				f.data = f.data[:size]
			} else {
				grown := make([]byte, size)
				copy(grown, f.data)
				f.data = grown
			}
		case attr.AttrMode:
			f.mode = d.Uint32()
		default:
			return fmt.Errorf("memRWProto: unsupported settable attr %d", num)
		}
	}
	return d.Err()
}

// Compile-time checks that the fake satisfies the interfaces.
var (
	_ RWProtocol = (*memRWProto)(nil)
	_ Protocol   = (*memRWProto)(nil)
	_ fs.FileInfo
)
