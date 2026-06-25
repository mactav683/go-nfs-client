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
	"time"

	"github.com/mactav683/go-nfs-client/xdr"
)

// time_how4 values for settable times.
const (
	setToServerTime4 uint32 = 0
	setToClientTime4 uint32 = 1
)

// SettableAttrs describes the attributes a client may set via SETATTR or supply
// as create attributes. A nil field is left unset (absent from the bitmap).
type SettableAttrs struct {
	Mode       *uint32
	Size       *uint64
	TimeAccess *time.Time // nil = unset; set to client time
	TimeModify *time.Time
}

// EncodeFattr builds a fattr4 (bitmap4 + attrlist4 values) from the settable
// attributes. Values are emitted in increasing attribute-number order as
// required by the wire format. The returned mask is the attribute bitmap and
// vals is the packed attrlist4 byte stream.
func EncodeFattr(s SettableAttrs) (Bitmap, []byte) {
	var nums []AttrNum
	if s.Size != nil {
		nums = append(nums, AttrSize)
	}
	if s.Mode != nil {
		nums = append(nums, AttrMode)
	}
	if s.TimeAccess != nil {
		nums = append(nums, AttrTimeAccess)
	}
	if s.TimeModify != nil {
		nums = append(nums, AttrTimeModify)
	}
	mask := BitmapFor(nums...)

	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	// Emit in increasing attribute-number order.
	if s.Size != nil {
		e.Uint64(*s.Size)
	}
	if s.Mode != nil {
		e.Uint32(*s.Mode)
	}
	if s.TimeAccess != nil {
		encodeSettime(e, *s.TimeAccess)
	}
	if s.TimeModify != nil {
		encodeSettime(e, *s.TimeModify)
	}
	return mask, buf.Bytes()
}

// encodeSettime writes a settime4: SET_TO_CLIENT_TIME4 discriminant followed by
// an nfstime4{ int64 seconds; uint32 nseconds }.
func encodeSettime(e *xdr.Encoder, t time.Time) {
	e.Uint32(setToClientTime4)
	e.Int64(t.Unix())
	e.Uint32(uint32(t.Nanosecond()))
}
