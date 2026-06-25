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

//go:build integration

package nfs4

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mactav683/go-nfs-client/nfs4/attr"
	"github.com/mactav683/go-nfs-client/rpc"
)

func serverAddr() string {
	if a := os.Getenv("NFS_SERVER_ADDR"); a != "" {
		return a
	}
	return "127.0.0.1:2049"
}

// exportPath returns the slash-separated path to the test export beneath the
// server pseudo-root (default "/export").
func exportPath() []string {
	p := os.Getenv("NFS_EXPORT")
	if p == "" {
		p = "/export"
	}
	return strings.Split(strings.Trim(p, "/"), "/")
}

func authSys() rpc.OpaqueAuth {
	return rpc.AuthSys{
		Stamp:       uint32(time.Now().Unix()),
		MachineName: "go-nfs-test",
		UID:         0,
		GID:         0,
		GIDs:        []uint32{0},
	}.OpaqueAuth()
}

// TestMount verifies the v4.0 bootstrap against a live server: Mount returns a
// confirmed clientid and a non-empty root filehandle, and resolving the export
// path yields a usable filehandle.
func TestMount(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := Dial(ctx, serverAddr(), authSys(), ConnConfig{ClientName: "go-nfs-test"})
	if err != nil {
		t.Fatalf("Dial/Mount: %v", err)
	}
	defer conn.Close()

	if conn.ClientID() == 0 {
		t.Fatalf("expected non-zero confirmed clientid")
	}
	if len(conn.RootFH()) == 0 {
		t.Fatalf("expected non-empty root filehandle")
	}

	fh, err := conn.LookupPath(ctx, exportPath())
	if err != nil {
		t.Fatalf("LookupPath(%v): %v", exportPath(), err)
	}
	if len(fh) == 0 {
		t.Fatalf("expected non-empty export filehandle")
	}
}

// TestMountNonexistentExport confirms a missing path returns a mapped error,
// not a panic.
func TestMountNonexistentExport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := Dial(ctx, serverAddr(), authSys(), ConnConfig{ClientName: "go-nfs-test"})
	if err != nil {
		t.Fatalf("Dial/Mount: %v", err)
	}
	defer conn.Close()

	_, err = conn.LookupPath(ctx, []string{"definitely-not-here-12345"})
	if err == nil {
		t.Fatalf("expected error looking up nonexistent path")
	}
}

// existingFile returns the name and expected content of a file that is
// provisioned on the server independently of the client (the integration
// harness seeds it before the tests run). When NFS_EXISTING_FILE is unset the
// test that uses it skips, so the suite still runs against arbitrary servers.
func existingFile() (name, content string, ok bool) {
	name = os.Getenv("NFS_EXISTING_FILE")
	if name == "" {
		return "", "", false
	}
	return name, os.Getenv("NFS_EXISTING_CONTENT"), true
}

// TestExistingFilePresent asserts that, immediately after connecting to the NFS
// share, a pre-existing file (created out-of-band on the server) is visible:
// it resolves via LOOKUP under the export, GETATTR reports a regular file, and
// its contents read back exactly. This proves the client observes server state
// it did not itself create.
func TestExistingFilePresent(t *testing.T) {
	name, want, ok := existingFile()
	if !ok {
		t.Skip("set NFS_EXISTING_FILE (and NFS_EXISTING_CONTENT) to run; the harness seeds it")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := Dial(ctx, serverAddr(), authSys(), ConnConfig{ClientName: "go-nfs-test"})
	if err != nil {
		t.Fatalf("Dial/Mount: %v", err)
	}
	defer conn.Close()

	dir, err := conn.LookupPath(ctx, exportPath())
	if err != nil {
		t.Fatalf("LookupPath(export): %v", err)
	}

	// The file must already exist under the export.
	fh, err := conn.Lookup(ctx, dir, name)
	if err != nil {
		t.Fatalf("pre-existing file %q not found via LOOKUP: %v", name, err)
	}

	// GETATTR must report a regular file with the expected size.
	mask, vals, err := conn.GetAttr(ctx, fh, Bitmap(attr.StandardMask()))
	if err != nil {
		t.Fatalf("GetAttr(%q): %v", name, err)
	}
	attrs, err := attr.Decode(attr.Bitmap(mask), vals)
	if err != nil {
		t.Fatalf("decoding attributes for %q: %v", name, err)
	}
	if attrs.Type != attr.FtypeReg {
		t.Fatalf("%q type = %v, want regular file (FtypeReg)", name, attrs.Type)
	}
	if want != "" && attrs.Size != uint64(len(want)) {
		t.Fatalf("%q size = %d, want %d", name, attrs.Size, len(want))
	}

	// Contents must read back exactly.
	if want != "" {
		var got []byte
		off := uint64(0)
		for {
			data, eof, rerr := conn.Read(ctx, fh, off, 8192)
			if rerr != nil {
				t.Fatalf("Read(%q): %v", name, rerr)
			}
			got = append(got, data...)
			off += uint64(len(data))
			if eof || len(data) == 0 {
				break
			}
		}
		if !bytes.Equal(got, []byte(want)) {
			t.Fatalf("%q content = %q, want %q", name, got, want)
		}
	}
}
