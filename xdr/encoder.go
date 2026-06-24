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
	"io"
)

// Unit is the XDR basic block size in bytes (RFC 4506 §3): all data is encoded
// in multiples of four bytes.
const Unit = 4

// Encoder serializes Go values into the XDR (RFC 4506) wire representation.
//
// Encoder follows the sticky-error pattern: once a write fails, every
// subsequent operation is a no-op and the original error is retained. Callers
// encode a sequence of fields and then check Err once at the end.
type Encoder struct {
	w   io.Writer
	err error
	// buf is a small scratch buffer reused across primitive writes to avoid
	// per-call allocations.
	buf [8]byte
}

// NewEncoder returns an Encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

// Err returns the first error encountered during encoding, if any.
func (e *Encoder) Err() error {
	return e.err
}

// write emits p to the underlying writer, latching any error.
func (e *Encoder) write(p []byte) {
	if e.err != nil {
		return
	}
	if _, err := e.w.Write(p); err != nil {
		e.err = err
	}
}

// pad emits n zero bytes (0 <= n < 4) to align to a 4-byte boundary.
func (e *Encoder) pad(n int) {
	if n == 0 {
		return
	}
	var zero [Unit]byte
	e.write(zero[:n])
}

// Uint32 encodes a 32-bit unsigned integer in big-endian order.
func (e *Encoder) Uint32(v uint32) {
	binary.BigEndian.PutUint32(e.buf[:4], v)
	e.write(e.buf[:4])
}

// Int32 encodes a signed 32-bit integer (two's complement, big-endian).
func (e *Encoder) Int32(v int32) {
	e.Uint32(uint32(v))
}

// Uint64 encodes a 64-bit unsigned integer in big-endian order.
func (e *Encoder) Uint64(v uint64) {
	binary.BigEndian.PutUint64(e.buf[:8], v)
	e.write(e.buf[:8])
}

// Int64 encodes a signed 64-bit integer (two's complement, big-endian).
func (e *Encoder) Int64(v int64) {
	e.Uint64(uint64(v))
}

// Bool encodes an XDR boolean as a 32-bit integer (0 = false, 1 = true).
func (e *Encoder) Bool(v bool) {
	if v {
		e.Uint32(1)
	} else {
		e.Uint32(0)
	}
}

// Opaque encodes a variable-length opaque value: a uint32 length prefix
// followed by the bytes padded with zeros up to a 4-byte boundary.
func (e *Encoder) Opaque(p []byte) {
	e.Uint32(uint32(len(p)))
	e.write(p)
	e.pad(pad4(len(p)))
}

// FixedOpaque encodes a fixed-length opaque value: the bytes padded with zeros
// up to a 4-byte boundary, with no length prefix.
func (e *Encoder) FixedOpaque(p []byte) {
	e.write(p)
	e.pad(pad4(len(p)))
}

// String encodes a variable-length string identically to a variable-length
// opaque value.
func (e *Encoder) String(s string) {
	e.Uint32(uint32(len(s)))
	e.write([]byte(s))
	e.pad(pad4(len(s)))
}

// Union encodes a discriminated union: the uint32 discriminant followed by the
// arm body emitted by encodeArm.
func (e *Encoder) Union(discriminant uint32, encodeArm func()) {
	e.Uint32(discriminant)
	if encodeArm != nil {
		encodeArm()
	}
}

// pad4 returns the number of padding bytes needed to round n up to a multiple
// of four.
func pad4(n int) int {
	r := n % Unit
	if r == 0 {
		return 0
	}
	return Unit - r
}
