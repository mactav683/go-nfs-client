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
	"bytes"
	"io"
	"testing"
)

// TestWriteRecord asserts the 4-byte record marker: high bit set for the last
// (and only) fragment, low 31 bits carry the payload length.
func TestWriteRecord(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	if err := WriteRecord(&buf, payload); err != nil {
		t.Fatalf("WriteRecord: %v", err)
	}
	// marker = 0x80000000 | 4 = 0x80000004
	want := []byte{0x80, 0x00, 0x00, 0x04, 0xDE, 0xAD, 0xBE, 0xEF}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("WriteRecord = % x, want % x", buf.Bytes(), want)
	}
}

// TestReadRecordSingleFragment reads back a single last-fragment record.
func TestReadRecordSingleFragment(t *testing.T) {
	in := []byte{0x80, 0x00, 0x00, 0x04, 0xDE, 0xAD, 0xBE, 0xEF}
	got, err := ReadRecord(bytes.NewReader(in))
	if err != nil {
		t.Fatalf("ReadRecord: %v", err)
	}
	want := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	if !bytes.Equal(got, want) {
		t.Fatalf("ReadRecord = % x, want % x", got, want)
	}
}

// TestReadRecordMultiFragment reassembles a message split across two
// fragments: the first without the last-fragment bit, the second with it.
func TestReadRecordMultiFragment(t *testing.T) {
	var in bytes.Buffer
	// Fragment 1: length 2, not last -> marker = 0x00000002
	in.Write([]byte{0x00, 0x00, 0x00, 0x02, 0xAA, 0xBB})
	// Fragment 2: length 3, last -> marker = 0x80000003
	in.Write([]byte{0x80, 0x00, 0x00, 0x03, 0xCC, 0xDD, 0xEE})

	got, err := ReadRecord(&in)
	if err != nil {
		t.Fatalf("ReadRecord: %v", err)
	}
	want := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE}
	if !bytes.Equal(got, want) {
		t.Fatalf("multi-fragment ReadRecord = % x, want % x", got, want)
	}
}

// TestRecordRoundTrip writes then reads a payload back unchanged.
func TestRecordRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := bytes.Repeat([]byte{0x5A}, 137)
	if err := WriteRecord(&buf, payload); err != nil {
		t.Fatalf("WriteRecord: %v", err)
	}
	got, err := ReadRecord(&buf)
	if err != nil {
		t.Fatalf("ReadRecord: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("round-trip mismatch")
	}
}

// TestReadRecordTruncated surfaces an error rather than hanging or panicking
// when the stream ends mid-record.
func TestReadRecordTruncated(t *testing.T) {
	in := []byte{0x80, 0x00, 0x00, 0x04, 0xDE, 0xAD} // claims 4 bytes, has 2
	_, err := ReadRecord(bytes.NewReader(in))
	if err == nil || err == io.EOF {
		t.Fatalf("expected unexpected-EOF error, got %v", err)
	}
}
