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

package rpc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Record-marking constants (RFC 5531 §11). Each fragment is prefixed by a
// 4-byte header: the high bit marks the last fragment of a message, and the
// low 31 bits give the fragment's byte length.
const (
	lastFragmentFlag uint32 = 0x80000000
	fragmentSizeMask uint32 = 0x7FFFFFFF

	// maxFragmentSize bounds a single fragment to guard against hostile or
	// corrupt length headers. NFSv4 servers negotiate far smaller transfer
	// sizes; 256 MiB is a generous ceiling.
	maxFragmentSize = 256 << 20
	// maxMessageSize bounds the total reassembled message size.
	maxMessageSize = 512 << 20
)

// ErrFragmentTooLarge indicates a fragment or message exceeded the configured
// size limits.
var ErrFragmentTooLarge = errors.New("rpc: fragment exceeds maximum size")

// WriteRecord writes payload as a single last-fragment record: a 4-byte marker
// (last-fragment flag | length) followed by the payload bytes.
func WriteRecord(w io.Writer, payload []byte) error {
	if uint32(len(payload)) > fragmentSizeMask {
		return ErrFragmentTooLarge
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], lastFragmentFlag|uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	return nil
}

// ReadRecord reads a complete RPC message from r, reassembling all fragments
// until the last-fragment flag is seen, and returns the concatenated payload.
func ReadRecord(r io.Reader) ([]byte, error) {
	var msg []byte
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, err
			}
			if errors.Is(err, io.EOF) && len(msg) == 0 {
				return nil, io.EOF
			}
			return nil, err
		}
		marker := binary.BigEndian.Uint32(hdr[:])
		last := marker&lastFragmentFlag != 0
		size := marker & fragmentSizeMask

		if size > maxFragmentSize {
			return nil, fmt.Errorf("%w: %d bytes", ErrFragmentTooLarge, size)
		}
		if uint64(len(msg))+uint64(size) > maxMessageSize {
			return nil, fmt.Errorf("%w: reassembled message too large", ErrFragmentTooLarge)
		}

		frag := make([]byte, size)
		if _, err := io.ReadFull(r, frag); err != nil {
			if errors.Is(err, io.EOF) {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		msg = append(msg, frag...)

		if last {
			return msg, nil
		}
	}
}
