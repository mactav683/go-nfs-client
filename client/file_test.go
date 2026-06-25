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

package client

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
)

// The File handle is exercised against an in-memory write-capable fake protocol
// (memRWProto) and its behavior is compared, where practical, against os.File.

func newRWFS(t *testing.T) *RWFS {
	t.Helper()
	return NewRW(newMemRWProto())
}

func TestFileWriteReadAt(t *testing.T) {
	fsys := newRWFS(t)
	f, err := fsys.OpenFile(context.Background(), "data.bin", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer f.Close()

	if _, err := f.WriteAt([]byte("hello world"), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	buf := make([]byte, 5)
	n, err := f.ReadAt(buf, 6)
	if err != nil && err != io.EOF {
		t.Fatalf("ReadAt: %v", err)
	}
	if string(buf[:n]) != "world" {
		t.Fatalf("ReadAt = %q, want %q", buf[:n], "world")
	}
}

func TestFileSeekRead(t *testing.T) {
	fsys := newRWFS(t)
	f, _ := fsys.OpenFile(context.Background(), "seek.bin", os.O_CREATE|os.O_RDWR, 0o644)
	defer f.Close()
	f.WriteAt([]byte("0123456789"), 0)

	if _, err := f.Seek(4, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	buf := make([]byte, 3)
	n, _ := f.Read(buf)
	if string(buf[:n]) != "456" {
		t.Fatalf("after seek read = %q, want 456", buf[:n])
	}
}

func TestFileTruncateShrinkGrow(t *testing.T) {
	fsys := newRWFS(t)
	f, _ := fsys.OpenFile(context.Background(), "trunc.bin", os.O_CREATE|os.O_RDWR, 0o644)
	defer f.Close()
	f.WriteAt([]byte("abcdefgh"), 0)

	if err := f.Truncate(4); err != nil {
		t.Fatalf("Truncate shrink: %v", err)
	}
	fi, _ := f.Stat()
	if fi.Size() != 4 {
		t.Fatalf("size after shrink = %d, want 4", fi.Size())
	}

	if err := f.Truncate(8); err != nil {
		t.Fatalf("Truncate grow: %v", err)
	}
	got := make([]byte, 8)
	n, _ := f.ReadAt(got, 0)
	// grow zero-fills
	want := append([]byte("abcd"), 0, 0, 0, 0)
	if !bytes.Equal(got[:n], want) {
		t.Fatalf("after grow = % x, want % x", got[:n], want)
	}
}

func TestFileSync(t *testing.T) {
	fsys := newRWFS(t)
	f, _ := fsys.OpenFile(context.Background(), "sync.bin", os.O_CREATE|os.O_RDWR, 0o644)
	defer f.Close()
	f.WriteAt([]byte("x"), 0)
	if err := f.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

// TestOpenFlagParity compares O_CREATE/O_TRUNC/O_APPEND/O_EXCL outcomes against
// os.OpenFile over a local temp dir.
func TestOpenFlagParity(t *testing.T) {
	tmp := t.TempDir()
	ctx := context.Background()

	t.Run("O_CREATE", func(t *testing.T) {
		fsys := newRWFS(t)
		f, err := fsys.OpenFile(ctx, "c.txt", os.O_CREATE|os.O_RDWR, 0o644)
		osf, oerr := os.OpenFile(tmp+"/c.txt", os.O_CREATE|os.O_RDWR, 0o644)
		if (err == nil) != (oerr == nil) {
			t.Fatalf("O_CREATE parity: nfs err=%v os err=%v", err, oerr)
		}
		f.Close()
		osf.Close()
	})

	t.Run("O_EXCL_existing", func(t *testing.T) {
		fsys := newRWFS(t)
		f1, _ := fsys.OpenFile(ctx, "e.txt", os.O_CREATE|os.O_RDWR, 0o644)
		f1.Close()
		_, err := fsys.OpenFile(ctx, "e.txt", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)

		osf1, _ := os.OpenFile(tmp+"/e.txt", os.O_CREATE|os.O_RDWR, 0o644)
		osf1.Close()
		_, oerr := os.OpenFile(tmp+"/e.txt", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)

		if (err == nil) != (oerr == nil) {
			t.Fatalf("O_EXCL parity: nfs err=%v os err=%v", err, oerr)
		}
		if !os.IsExist(err) {
			t.Fatalf("expected IsExist for O_EXCL, got %v", err)
		}
	})

	t.Run("O_TRUNC", func(t *testing.T) {
		fsys := newRWFS(t)
		f1, _ := fsys.OpenFile(ctx, "t.txt", os.O_CREATE|os.O_RDWR, 0o644)
		f1.WriteAt([]byte("longcontent"), 0)
		f1.Close()
		f2, err := fsys.OpenFile(ctx, "t.txt", os.O_TRUNC|os.O_RDWR, 0o644)
		if err != nil {
			t.Fatalf("O_TRUNC open: %v", err)
		}
		fi, _ := f2.Stat()
		if fi.Size() != 0 {
			t.Fatalf("O_TRUNC size = %d, want 0", fi.Size())
		}
		f2.Close()
	})

	t.Run("O_APPEND", func(t *testing.T) {
		fsys := newRWFS(t)
		f, _ := fsys.OpenFile(ctx, "a.txt", os.O_CREATE|os.O_RDWR, 0o644)
		f.WriteAt([]byte("base"), 0)
		f.Close()
		fa, err := fsys.OpenFile(ctx, "a.txt", os.O_APPEND|os.O_RDWR, 0o644)
		if err != nil {
			t.Fatalf("O_APPEND open: %v", err)
		}
		if _, err := fa.Write([]byte("+more")); err != nil {
			t.Fatalf("append Write: %v", err)
		}
		fa.Close()
		fr, _ := fsys.OpenFile(ctx, "a.txt", os.O_RDONLY, 0)
		got, _ := io.ReadAll(fr)
		fr.Close()
		if string(got) != "base+more" {
			t.Fatalf("append content = %q, want base+more", got)
		}
	})
}

func TestStatvfs(t *testing.T) {
	fsys := newRWFS(t)
	st, err := fsys.Statvfs(context.Background(), ".")
	if err != nil {
		t.Fatalf("Statvfs: %v", err)
	}
	if st.Blocks == 0 || st.BlockSize == 0 {
		t.Fatalf("statvfs returned implausible zero totals: %+v", st)
	}
}
