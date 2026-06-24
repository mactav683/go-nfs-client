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
	"context"
	"os"
	"strings"
	"testing"
	"time"

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
