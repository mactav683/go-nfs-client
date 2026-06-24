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

package xdr

import (
	"encoding/binary"
	"errors"
	"io"
)

// ErrLimitExceeded is returned when a length-prefixed field declares a size
// larger than the decoder's configured MaxLen, guarding against hostile or
// corrupt input that would otherwise trigger huge allocations.
var ErrLimitExceeded = errors.New("xdr: declared length exceeds maximum")

// defaultMaxLen caps the size of a single variable-length field (opaque or
// string). 64 MiB is far above any legitimate NFSv4 field and well below a
// memory-exhaustion threat.
const defaultMaxLen = 64 << 20

// Decoder deserializes XDR (RFC 4506) wire data into Go values.
//
// Decoder follows the sticky-error pattern: once a read fails, every
// subsequent operation returns a zero value and the original error is
// retained. Callers decode a sequence of fields and then check Err once.
type Decoder struct {
	r io.Reader
	// MaxLen bounds the declared length of variable-length fields. Zero means
	// use defaultMaxLen.
	MaxLen uint32
	err    error
	buf    [8]byte
}

// NewDecoder returns a Decoder that reads from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: r}
}

// Err returns the first error encountered during decoding, if any.
func (d *Decoder) Err() error {
	return d.err
}

func (d *Decoder) maxLen() uint32 {
	if d.MaxLen == 0 {
		return defaultMaxLen
	}
	return d.MaxLen
}

// read fills p completely, latching any error. A partial read at EOF is
// reported as io.ErrUnexpectedEOF via io.ReadFull.
func (d *Decoder) read(p []byte) {
	if d.err != nil {
		return
	}
	if _, err := io.ReadFull(d.r, p); err != nil {
		d.err = err
	}
}

// discard consumes and ignores n bytes (used for padding).
func (d *Decoder) discard(n int) {
	if n == 0 || d.err != nil {
		return
	}
	var scratch [Unit]byte
	d.read(scratch[:n])
}

// Uint32 decodes a 32-bit unsigned integer in big-endian order.
func (d *Decoder) Uint32() uint32 {
	d.read(d.buf[:4])
	if d.err != nil {
		return 0
	}
	return binary.BigEndian.Uint32(d.buf[:4])
}

// Int32 decodes a signed 32-bit integer (two's complement, big-endian).
func (d *Decoder) Int32() int32 {
	return int32(d.Uint32())
}

// Uint64 decodes a 64-bit unsigned integer in big-endian order.
func (d *Decoder) Uint64() uint64 {
	d.read(d.buf[:8])
	if d.err != nil {
		return 0
	}
	return binary.BigEndian.Uint64(d.buf[:8])
}

// Int64 decodes a signed 64-bit integer (two's complement, big-endian).
func (d *Decoder) Int64() int64 {
	return int64(d.Uint64())
}

// Bool decodes an XDR boolean (0 = false, anything else treated as true).
func (d *Decoder) Bool() bool {
	return d.Uint32() != 0
}

// Opaque decodes a variable-length opaque value: a uint32 length prefix, the
// bytes, and the padding consumed up to a 4-byte boundary.
func (d *Decoder) Opaque() []byte {
	n := d.Uint32()
	if d.err != nil {
		return nil
	}
	if n > d.maxLen() {
		d.err = ErrLimitExceeded
		return nil
	}
	out := make([]byte, n)
	d.read(out)
	if d.err != nil {
		return nil
	}
	d.discard(pad4(int(n)))
	if d.err != nil {
		return nil
	}
	return out
}

// FixedOpaque decodes exactly n bytes of fixed-length opaque data, consuming
// the padding up to a 4-byte boundary.
func (d *Decoder) FixedOpaque(n int) []byte {
	if n < 0 {
		d.err = ErrLimitExceeded
		return nil
	}
	out := make([]byte, n)
	d.read(out)
	if d.err != nil {
		return nil
	}
	d.discard(pad4(n))
	if d.err != nil {
		return nil
	}
	return out
}

// String decodes a variable-length string (same wire form as opaque).
func (d *Decoder) String() string {
	return string(d.Opaque())
}

// Union decodes a discriminated union: it reads the uint32 discriminant,
// invokes decodeArm with it, and returns the discriminant. decodeArm is
// responsible for decoding the selected arm's body.
func (d *Decoder) Union(decodeArm func(discriminant uint32)) uint32 {
	disc := d.Uint32()
	if d.err != nil {
		return 0
	}
	if decodeArm != nil {
		decodeArm(disc)
	}
	return disc
}
