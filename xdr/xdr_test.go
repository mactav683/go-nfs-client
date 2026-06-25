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
	"bytes"
	"testing"
	"testing/quick"
)

// TestModuleCompiles is a baseline test ensuring the package compiles.
func TestModuleCompiles(t *testing.T) {}

// encodeBytes is a helper that runs fn against a fresh Encoder and returns the
// produced bytes, failing the test on any encode error.
func encodeBytes(t *testing.T, fn func(e *Encoder)) []byte {
	t.Helper()
	var buf bytes.Buffer
	e := NewEncoder(&buf)
	fn(e)
	if err := e.Err(); err != nil {
		t.Fatalf("unexpected encode error: %v", err)
	}
	return buf.Bytes()
}

func TestEncodeUint32(t *testing.T) {
	cases := []struct {
		name string
		in   uint32
		want []byte
	}{
		{"zero", 0, []byte{0x00, 0x00, 0x00, 0x00}},
		{"one", 1, []byte{0x00, 0x00, 0x00, 0x01}},
		{"max", 0xFFFFFFFF, []byte{0xFF, 0xFF, 0xFF, 0xFF}},
		{"mixed", 0x01020304, []byte{0x01, 0x02, 0x03, 0x04}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := encodeBytes(t, func(e *Encoder) { e.Uint32(tc.in) })
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("Uint32(%#x) = % x, want % x", tc.in, got, tc.want)
			}
		})
	}
}

func TestEncodeUint64(t *testing.T) {
	got := encodeBytes(t, func(e *Encoder) { e.Uint64(0x0102030405060708) })
	want := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	if !bytes.Equal(got, want) {
		t.Fatalf("Uint64 = % x, want % x", got, want)
	}
}

func TestEncodeInt32(t *testing.T) {
	// -1 in two's complement is 0xFFFFFFFF (same wire form as uint32 max).
	got := encodeBytes(t, func(e *Encoder) { e.Int32(-1) })
	want := []byte{0xFF, 0xFF, 0xFF, 0xFF}
	if !bytes.Equal(got, want) {
		t.Fatalf("Int32(-1) = % x, want % x", got, want)
	}
}

func TestEncodeBool(t *testing.T) {
	if got := encodeBytes(t, func(e *Encoder) { e.Bool(false) }); !bytes.Equal(got, []byte{0, 0, 0, 0}) {
		t.Fatalf("Bool(false) = % x", got)
	}
	if got := encodeBytes(t, func(e *Encoder) { e.Bool(true) }); !bytes.Equal(got, []byte{0, 0, 0, 1}) {
		t.Fatalf("Bool(true) = % x", got)
	}
}

func TestEncodeFixedOpaquePadding(t *testing.T) {
	// Variable-length opaque (length-prefixed) padding edge cases: 1, 2, 3, 4
	// data bytes must each pad up to a 4-byte boundary.
	cases := []struct {
		name string
		in   []byte
		want []byte
	}{
		{"empty", []byte{}, []byte{0, 0, 0, 0}},
		{"one", []byte{0xAA}, []byte{0, 0, 0, 1, 0xAA, 0, 0, 0}},
		{"two", []byte{0xAA, 0xBB}, []byte{0, 0, 0, 2, 0xAA, 0xBB, 0, 0}},
		{"three", []byte{0xAA, 0xBB, 0xCC}, []byte{0, 0, 0, 3, 0xAA, 0xBB, 0xCC, 0}},
		{"four", []byte{0xAA, 0xBB, 0xCC, 0xDD}, []byte{0, 0, 0, 4, 0xAA, 0xBB, 0xCC, 0xDD}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := encodeBytes(t, func(e *Encoder) { e.Opaque(tc.in) })
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("Opaque(% x) = % x, want % x", tc.in, got, tc.want)
			}
		})
	}
}

func TestEncodeFixedOpaqueNoLengthPrefix(t *testing.T) {
	// Fixed-length opaque carries no length prefix, only padding.
	got := encodeBytes(t, func(e *Encoder) { e.FixedOpaque([]byte{0xAA, 0xBB, 0xCC}) })
	want := []byte{0xAA, 0xBB, 0xCC, 0x00}
	if !bytes.Equal(got, want) {
		t.Fatalf("FixedOpaque = % x, want % x", got, want)
	}
}

func TestEncodeString(t *testing.T) {
	got := encodeBytes(t, func(e *Encoder) { e.String("hi") })
	want := []byte{0, 0, 0, 2, 'h', 'i', 0, 0}
	if !bytes.Equal(got, want) {
		t.Fatalf("String = % x, want % x", got, want)
	}
}

func TestRoundTripUint32(t *testing.T) {
	for _, v := range []uint32{0, 1, 42, 0x80000000, 0xFFFFFFFF} {
		b := encodeBytes(t, func(e *Encoder) { e.Uint32(v) })
		d := NewDecoder(bytes.NewReader(b))
		got := d.Uint32()
		if err := d.Err(); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if got != v {
			t.Fatalf("round-trip Uint32: got %#x want %#x", got, v)
		}
	}
}

func TestRoundTripUint64(t *testing.T) {
	for _, v := range []uint64{0, 1, 0x0102030405060708, 0xFFFFFFFFFFFFFFFF} {
		b := encodeBytes(t, func(e *Encoder) { e.Uint64(v) })
		d := NewDecoder(bytes.NewReader(b))
		got := d.Uint64()
		if err := d.Err(); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if got != v {
			t.Fatalf("round-trip Uint64: got %#x want %#x", got, v)
		}
	}
}

func TestRoundTripOpaque(t *testing.T) {
	for _, v := range [][]byte{{}, {1}, {1, 2}, {1, 2, 3}, {1, 2, 3, 4}, {1, 2, 3, 4, 5}} {
		b := encodeBytes(t, func(e *Encoder) { e.Opaque(v) })
		d := NewDecoder(bytes.NewReader(b))
		got := d.Opaque()
		if err := d.Err(); err != nil {
			t.Fatalf("decode error for % x: %v", v, err)
		}
		if !bytes.Equal(got, v) {
			t.Fatalf("round-trip Opaque: got % x want % x", got, v)
		}
	}
}

func TestRoundTripString(t *testing.T) {
	for _, v := range []string{"", "a", "ab", "abc", "abcd", "hello world"} {
		b := encodeBytes(t, func(e *Encoder) { e.String(v) })
		d := NewDecoder(bytes.NewReader(b))
		got := d.String()
		if err := d.Err(); err != nil {
			t.Fatalf("decode error for %q: %v", v, err)
		}
		if got != v {
			t.Fatalf("round-trip String: got %q want %q", got, v)
		}
	}
}

// TestUnion exercises the discriminated-union helper: a uint32 discriminant
// followed by an arm chosen by that discriminant.
func TestUnion(t *testing.T) {
	const armA = uint32(1)
	const armB = uint32(2)

	got := encodeBytes(t, func(e *Encoder) {
		e.Union(armB, func() { e.Uint32(0xDEADBEEF) })
	})
	want := []byte{0, 0, 0, 2, 0xDE, 0xAD, 0xBE, 0xEF}
	if !bytes.Equal(got, want) {
		t.Fatalf("Union encode = % x, want % x", got, want)
	}

	d := NewDecoder(bytes.NewReader(got))
	var payload uint32
	disc := d.Union(func(disc uint32) {
		switch disc {
		case armA:
			t.Fatalf("decoded wrong arm A")
		case armB:
			payload = d.Uint32()
		default:
			t.Fatalf("unexpected discriminant %d", disc)
		}
	})
	if err := d.Err(); err != nil {
		t.Fatalf("union decode error: %v", err)
	}
	if disc != armB {
		t.Fatalf("union discriminant = %d, want %d", disc, armB)
	}
	if payload != 0xDEADBEEF {
		t.Fatalf("union payload = %#x, want 0xDEADBEEF", payload)
	}
}

// TestDecodeTruncated ensures the decoder surfaces an error (rather than
// panicking) when the input is shorter than required.
func TestDecodeTruncated(t *testing.T) {
	d := NewDecoder(bytes.NewReader([]byte{0x00, 0x01}))
	_ = d.Uint32()
	if d.Err() == nil {
		t.Fatalf("expected error decoding truncated uint32")
	}
}

// FuzzXDRRoundTrip fuzzes opaque encode/decode round-trips.
func FuzzXDRRoundTrip(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{1, 2, 3})
	f.Add([]byte("hello world"))
	f.Fuzz(func(t *testing.T, data []byte) {
		var buf bytes.Buffer
		e := NewEncoder(&buf)
		e.Opaque(data)
		if err := e.Err(); err != nil {
			t.Fatalf("encode error: %v", err)
		}
		d := NewDecoder(bytes.NewReader(buf.Bytes()))
		got := d.Opaque()
		if err := d.Err(); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if !bytes.Equal(got, data) {
			t.Fatalf("round-trip mismatch: got % x want % x", got, data)
		}
	})
}

// TestQuickRoundTrip uses testing/quick to assert encode->decode is the
// identity for randomized uint64 values.
func TestQuickRoundTrip(t *testing.T) {
	f := func(v uint64) bool {
		var buf bytes.Buffer
		e := NewEncoder(&buf)
		e.Uint64(v)
		if e.Err() != nil {
			return false
		}
		d := NewDecoder(bytes.NewReader(buf.Bytes()))
		got := d.Uint64()
		return d.Err() == nil && got == v
	}
	if err := quick.Check(f, nil); err != nil {
		t.Fatal(err)
	}
}
