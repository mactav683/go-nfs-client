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
	"testing"

	"github.com/mactav683/go-nfs-client/xdr"
)

// TestAuthNull encodes the AUTH_NULL opaque_auth: flavor 0, empty body.
func TestAuthNull(t *testing.T) {
	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	AuthNull().Encode(e)
	if err := e.Err(); err != nil {
		t.Fatalf("encode: %v", err)
	}
	// flavor = 0, body length = 0
	want := []byte{0, 0, 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("AuthNull = % x, want % x", buf.Bytes(), want)
	}
}

// TestAuthSysCredential asserts the exact AUTH_SYS body byte layout per
// RFC 5531 §8.2: opaque_auth{ flavor=AUTH_SYS(1), body=opaque{ stamp,
// machinename<string>, uid, gid, gids<array> } }.
func TestAuthSysCredential(t *testing.T) {
	cred := AuthSys{
		Stamp:       0x11223344,
		MachineName: "host",
		UID:         1000,
		GID:         1000,
		GIDs:        []uint32{1000, 27},
	}

	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	cred.Encode(e)
	if err := e.Err(); err != nil {
		t.Fatalf("encode: %v", err)
	}

	// Build the expected body independently.
	//   stamp        = 0x11223344
	//   machinename  = len(4) "host"        -> 00000004 'h' 'o' 's' 't'
	//   uid          = 1000 (0x3E8)
	//   gid          = 1000
	//   gids         = count(2), 1000, 27
	body := []byte{
		0x11, 0x22, 0x33, 0x44, // stamp
		0x00, 0x00, 0x00, 0x04, 'h', 'o', 's', 't', // machinename
		0x00, 0x00, 0x03, 0xE8, // uid
		0x00, 0x00, 0x03, 0xE8, // gid
		0x00, 0x00, 0x00, 0x02, // gids count
		0x00, 0x00, 0x03, 0xE8, // gid 1000
		0x00, 0x00, 0x00, 0x1B, // gid 27
	}
	// opaque_auth wraps body: flavor=1, then body as opaque (length prefix).
	var want bytes.Buffer
	want.Write([]byte{0x00, 0x00, 0x00, 0x01}) // AUTH_SYS flavor
	// body length prefix
	want.Write([]byte{0x00, 0x00, 0x00, byte(len(body))})
	want.Write(body)

	if !bytes.Equal(buf.Bytes(), want.Bytes()) {
		t.Fatalf("AuthSys.Encode =\n% x\nwant\n% x", buf.Bytes(), want.Bytes())
	}
}

// TestAuthSysBodyIsMultipleOfFour guards that the machinename padding keeps the
// whole credential 4-byte aligned for an odd-length hostname.
func TestAuthSysBodyAlignment(t *testing.T) {
	cred := AuthSys{MachineName: "abcde"} // 5 bytes -> padded to 8
	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	cred.Encode(e)
	if err := e.Err(); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if buf.Len()%4 != 0 {
		t.Fatalf("AuthSys encoding length %d not 4-byte aligned", buf.Len())
	}
}
