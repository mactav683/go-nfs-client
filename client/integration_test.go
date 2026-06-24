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
	"io/fs"
	"os"
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

func exportName() string {
	if e := os.Getenv("NFS_EXPORT"); e != "" {
		return e
	}
	return "/export"
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

// TestReadPathLive mounts the export and exercises the io/fs read surface
// against the live server: WalkDir traversal and reading file contents.
func TestReadPathLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cli, err := Mount(ctx, serverAddr(), exportName(), authSys())
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	defer cli.Close()

	fsys := cli.FS(ctx)

	// WalkDir should traverse without error and visit at least the root.
	count := 0
	err = fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
	if count == 0 {
		t.Fatalf("WalkDir visited no entries")
	}

	// Opening a missing file returns fs.ErrNotExist.
	_, err = fsys.Open("definitely-not-here-98765.txt")
	if err == nil {
		t.Fatalf("expected error opening missing file")
	}
}
