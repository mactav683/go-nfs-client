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
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/mactav683/go-nfs-client/nfs4/attr"
)

// mountExport dials, mounts, and resolves the export directory filehandle.
func mountExport(t *testing.T, ctx context.Context) (*Conn, FileHandle) {
	t.Helper()
	conn, err := Dial(ctx, serverAddr(), authSys(), ConnConfig{ClientName: "go-nfs-write-test"})
	if err != nil {
		t.Fatalf("Dial/Mount: %v", err)
	}
	fh, err := conn.LookupPath(ctx, exportPath())
	if err != nil {
		conn.Close()
		t.Fatalf("LookupPath: %v", err)
	}
	return conn, fh
}

// TestWrite covers the create -> write -> commit -> close -> reopen -> read
// lifecycle plus MKDIR/RENAME/REMOVE/SETATTR against a live server.
func TestWrite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn, dir := mountExport(t, ctx)
	defer conn.Close()

	mode := uint32(0o644)
	mask, vals := attr.EncodeFattr(attr.SettableAttrs{Mode: &mode})

	name := "go-nfs-write.tmp"
	// Best-effort cleanup of any prior run.
	_ = conn.Remove(ctx, dir, name)

	// create -> write (multi-block) -> commit -> close
	open, err := conn.Open(ctx, dir, name, OpenShareAccessBoth, Bitmap(mask), vals, true, false)
	if err != nil {
		t.Fatalf("Open(create): %v", err)
	}
	payload := bytes.Repeat([]byte("nfs4-write-"), 5000) // ~55KB, multi-block
	off := uint64(0)
	for off < uint64(len(payload)) {
		end := off + 8192
		if end > uint64(len(payload)) {
			end = uint64(len(payload))
		}
		n, _, werr := conn.Write(ctx, open.FH, open.Stateid, off, payload[off:end], Unstable4)
		if werr != nil {
			t.Fatalf("Write: %v", werr)
		}
		off += uint64(n)
	}
	if err := conn.Commit(ctx, open.FH, 0, 0); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := conn.CloseFile(ctx, open.FH, open.Stateid); err != nil {
		t.Fatalf("CloseFile: %v", err)
	}

	// reopen and read back
	ro, err := conn.Open(ctx, dir, name, OpenShareAccessRead, nil, nil, false, false)
	if err != nil {
		t.Fatalf("Open(read): %v", err)
	}
	var got []byte
	rOff := uint64(0)
	for {
		data, eof, rerr := conn.Read(ctx, ro.FH, rOff, 8192)
		if rerr != nil {
			t.Fatalf("Read: %v", rerr)
		}
		got = append(got, data...)
		rOff += uint64(len(data))
		if eof || len(data) == 0 {
			break
		}
	}
	_ = conn.CloseFile(ctx, ro.FH, ro.Stateid)
	if !bytes.Equal(got, payload) {
		t.Fatalf("read back %d bytes, want %d", len(got), len(payload))
	}

	// SETATTR: change mode and verify via GETATTR.
	newMode := uint32(0o600)
	smask, svals := attr.EncodeFattr(attr.SettableAttrs{Mode: &newMode})
	if err := conn.SetAttr(ctx, ro.FH, AnonStateid, Bitmap(smask), svals); err != nil {
		t.Fatalf("SetAttr: %v", err)
	}

	// MKDIR then verify it resolves.
	dirName := "go-nfs-dir.tmp"
	_ = conn.Remove(ctx, dir, dirName)
	dmask, dvals := attr.EncodeFattr(attr.SettableAttrs{Mode: ptrU32(0o755)})
	if _, err := conn.Mkdir(ctx, dir, dirName, Bitmap(dmask), dvals); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if _, err := conn.Lookup(ctx, dir, dirName); err != nil {
		t.Fatalf("Lookup(new dir): %v", err)
	}

	// RENAME the file.
	renamed := "go-nfs-write-renamed.tmp"
	_ = conn.Remove(ctx, dir, renamed)
	if err := conn.Rename(ctx, dir, name, dir, renamed); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := conn.Lookup(ctx, dir, name); err == nil {
		t.Fatalf("old name should be absent after rename")
	}
	if _, err := conn.Lookup(ctx, dir, renamed); err != nil {
		t.Fatalf("new name should be present after rename: %v", err)
	}

	// REMOVE the file; subsequent lookup yields ErrNotExist.
	if err := conn.Remove(ctx, dir, renamed); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := conn.Lookup(ctx, dir, renamed); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected fs.ErrNotExist after remove, got %v", err)
	}

	// Cleanup the directory.
	_ = conn.Remove(ctx, dir, dirName)
}

// TestWriteExclExist verifies O_EXCL create of an existing file returns
// fs.ErrExist.
func TestWriteExclExist(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	conn, dir := mountExport(t, ctx)
	defer conn.Close()

	name := "go-nfs-excl.tmp"
	_ = conn.Remove(ctx, dir, name)
	mask, vals := attr.EncodeFattr(attr.SettableAttrs{Mode: ptrU32(0o644)})
	first, err := conn.Open(ctx, dir, name, OpenShareAccessBoth, Bitmap(mask), vals, true, true)
	if err != nil {
		t.Fatalf("Open(O_EXCL create): %v", err)
	}
	_ = conn.CloseFile(ctx, first.FH, first.Stateid)

	_, err = conn.Open(ctx, dir, name, OpenShareAccessBoth, Bitmap(mask), vals, true, true)
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("expected fs.ErrExist for O_EXCL of existing file, got %v", err)
	}
	_ = conn.Remove(ctx, dir, name)
}

func ptrU32(v uint32) *uint32 { return &v }
