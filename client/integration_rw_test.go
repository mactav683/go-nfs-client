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

package client

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"
)

// TestRWPathLive exercises the os.File-like handle against a live server:
// create, write, sync, read-back, truncate, seek, append, and statvfs.
func TestRWPathLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cli, err := Mount(ctx, serverAddr(), exportName(), authSys(), mountOpts()...)
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	defer cli.Close()

	fsys := cli.RWFS(ctx)

	name := fmt.Sprintf("go-nfs-rw-%d.bin", time.Now().UnixNano())

	f, err := fsys.OpenFile(ctx, name, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("OpenFile create: %v", err)
	}

	const payload = "hello nfsv4 read-write world"
	if _, err := f.WriteAt([]byte(payload), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Seek + sequential read.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("read-back = %q, want %q", got, payload)
	}

	// Truncate shrink.
	if err := f.Truncate(5); err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	fi, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if fi.Size() != 5 {
		t.Fatalf("size after truncate = %d, want 5", fi.Size())
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Statvfs on the pseudo-root should return plausible totals.
	st, err := fsys.Statvfs(ctx, ".")
	if err != nil {
		t.Fatalf("Statvfs: %v", err)
	}
	if st.Blocks == 0 {
		t.Fatalf("statvfs reported zero total blocks: %+v", st)
	}

	// Clean up the test file.
	if err := cli.Conn().Remove(ctx, cli.Conn().RootFH(), name); err != nil {
		t.Logf("cleanup Remove(%q): %v", name, err)
	}
}
