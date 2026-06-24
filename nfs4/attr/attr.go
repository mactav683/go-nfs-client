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

package attr

import (
	"bytes"
	"fmt"
	"io/fs"
	"time"

	"github.com/mactav683/go-nfs-client/xdr"
)

// Ftype is the nfs_ftype4 file-type enumeration (RFC 7530).
type Ftype uint32

// nfs_ftype4 values.
const (
	FtypeReg     Ftype = 1 // NF4REG: regular file
	FtypeDir     Ftype = 2 // NF4DIR: directory
	FtypeBlk     Ftype = 3 // NF4BLK: block device
	FtypeChr     Ftype = 4 // NF4CHR: character device
	FtypeLnk     Ftype = 5 // NF4LNK: symbolic link
	FtypeSock    Ftype = 6 // NF4SOCK: socket
	FtypeFifo    Ftype = 7 // NF4FIFO: named pipe
	FtypeAttrDir Ftype = 8 // NF4ATTRDIR: named attribute directory
	FtypeNamedAt Ftype = 9 // NF4NAMEDATTR: named attribute
)

// FSID is the fsid4 filesystem identifier.
type FSID struct {
	Major uint64
	Minor uint64
}

// SpecData is the specdata4 device numbers for block/character devices.
type SpecData struct {
	Spec1 uint32
	Spec2 uint32
}

// Attributes holds the decoded standard attribute set mapped to Go types.
// Fields that were not present in the source bitmap retain their zero value;
// Present records which attributes were actually decoded.
type Attributes struct {
	Present Bitmap

	Type       Ftype
	Size       uint64
	FSID       FSID
	FileID     uint64
	Mode       uint32
	NumLinks   uint32
	Owner      string
	OwnerGroup string
	RawDev     SpecData
	SpaceUsed  uint64
	TimeAccess time.Time
	TimeMeta   time.Time
	TimeModify time.Time
}

// Decode parses an attrlist4 (vals) according to mask, returning the typed
// Attributes. Values appear in increasing attribute-number order, so the mask's
// SetAttrs() ordering drives decoding.
func Decode(mask Bitmap, vals []byte) (*Attributes, error) {
	a := &Attributes{Present: mask}
	d := xdr.NewDecoder(bytes.NewReader(vals))

	for _, num := range mask.SetAttrs() {
		switch num {
		case AttrType:
			a.Type = Ftype(d.Uint32())
		case AttrSize:
			a.Size = d.Uint64()
		case AttrFSID:
			a.FSID.Major = d.Uint64()
			a.FSID.Minor = d.Uint64()
		case AttrFileID:
			a.FileID = d.Uint64()
		case AttrMode:
			a.Mode = d.Uint32()
		case AttrNumLinks:
			a.NumLinks = d.Uint32()
		case AttrOwner:
			a.Owner = d.String()
		case AttrOwnerGroup:
			a.OwnerGroup = d.String()
		case AttrRawDev:
			a.RawDev.Spec1 = d.Uint32()
			a.RawDev.Spec2 = d.Uint32()
		case AttrSpaceUsed:
			a.SpaceUsed = d.Uint64()
		case AttrTimeAccess:
			a.TimeAccess = decodeTime(d)
		case AttrTimeMetadata:
			a.TimeMeta = decodeTime(d)
		case AttrTimeModify:
			a.TimeModify = decodeTime(d)
		default:
			// Unknown/unsupported attribute in the mask: we cannot know its
			// length, so we must stop to avoid misaligned decoding.
			return nil, fmt.Errorf("attr: cannot decode unsupported attribute %d", num)
		}
		if err := d.Err(); err != nil {
			return nil, fmt.Errorf("attr: decoding attribute %d: %w", num, err)
		}
	}
	return a, nil
}

// decodeTime reads an nfstime4{ int64 seconds; uint32 nseconds } into a
// time.Time in UTC.
func decodeTime(d *xdr.Decoder) time.Time {
	secs := d.Int64()
	nsecs := d.Uint32()
	if d.Err() != nil {
		return time.Time{}
	}
	return time.Unix(secs, int64(nsecs)).UTC()
}

// FileMode maps the NFSv4 type and mode bits onto an os.FileMode.
func (a *Attributes) FileMode() fs.FileMode {
	mode := fs.FileMode(a.Mode & 0o777)
	// setuid/setgid/sticky.
	if a.Mode&0o4000 != 0 {
		mode |= fs.ModeSetuid
	}
	if a.Mode&0o2000 != 0 {
		mode |= fs.ModeSetgid
	}
	if a.Mode&0o1000 != 0 {
		mode |= fs.ModeSticky
	}
	switch a.Type {
	case FtypeDir, FtypeAttrDir:
		mode |= fs.ModeDir
	case FtypeLnk:
		mode |= fs.ModeSymlink
	case FtypeBlk:
		mode |= fs.ModeDevice
	case FtypeChr:
		mode |= fs.ModeDevice | fs.ModeCharDevice
	case FtypeSock:
		mode |= fs.ModeSocket
	case FtypeFifo:
		mode |= fs.ModeNamedPipe
	}
	return mode
}

// FileInfo returns an fs.FileInfo view of the attributes for the given name.
func (a *Attributes) FileInfo(name string) fs.FileInfo {
	return &fileInfo{name: name, attrs: a}
}

// fileInfo adapts Attributes to fs.FileInfo.
type fileInfo struct {
	name  string
	attrs *Attributes
}

func (fi *fileInfo) Name() string       { return fi.name }
func (fi *fileInfo) Size() int64        { return int64(fi.attrs.Size) }
func (fi *fileInfo) Mode() fs.FileMode  { return fi.attrs.FileMode() }
func (fi *fileInfo) ModTime() time.Time { return fi.attrs.TimeModify }
func (fi *fileInfo) IsDir() bool        { return fi.attrs.FileMode().IsDir() }
func (fi *fileInfo) Sys() any           { return fi.attrs }
