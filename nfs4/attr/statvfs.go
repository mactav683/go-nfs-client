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

	"github.com/mactav683/go-nfs-client/xdr"
)

// Statvfs holds the filesystem-statistics attribute set (the attributes
// requested by StatvfsMask), mapped to Go types. Space values are byte counts;
// file values are inode counts. Fields absent from the source bitmap retain
// their zero value.
type Statvfs struct {
	Present Bitmap

	FSID       FSID
	FilesAvail uint64 // files_avail: free inodes available to non-privileged users
	FilesFree  uint64 // files_free: total free inodes
	FilesTotal uint64 // files_total: total inodes
	MaxName    uint32 // maxname: maximum filename length
	SpaceAvail uint64 // space_avail: free bytes available to non-privileged users
	SpaceFree  uint64 // space_free: total free bytes
	SpaceTotal uint64 // space_total: total bytes
}

// DecodeStatvfs parses an attrlist4 (vals) carrying the statvfs attribute set
// according to mask. Values appear in increasing attribute-number order, so the
// mask's SetAttrs() ordering drives decoding.
func DecodeStatvfs(mask Bitmap, vals []byte) (*Statvfs, error) {
	s := &Statvfs{Present: mask}
	d := xdr.NewDecoder(bytes.NewReader(vals))

	for _, num := range mask.SetAttrs() {
		switch num {
		case AttrFSID:
			s.FSID.Major = d.Uint64()
			s.FSID.Minor = d.Uint64()
		case AttrFilesAvail:
			s.FilesAvail = d.Uint64()
		case AttrFilesFree:
			s.FilesFree = d.Uint64()
		case AttrFilesTotal:
			s.FilesTotal = d.Uint64()
		case AttrMaxName:
			s.MaxName = d.Uint32()
		case AttrSpaceAvail:
			s.SpaceAvail = d.Uint64()
		case AttrSpaceFree:
			s.SpaceFree = d.Uint64()
		case AttrSpaceTotal:
			s.SpaceTotal = d.Uint64()
		default:
			// Unknown/unsupported attribute in the mask: we cannot know its
			// length, so we must stop to avoid misaligned decoding.
			return nil, fmt.Errorf("attr: cannot decode unsupported statvfs attribute %d", num)
		}
		if err := d.Err(); err != nil {
			return nil, fmt.Errorf("attr: decoding statvfs attribute %d: %w", num, err)
		}
	}
	return s, nil
}
