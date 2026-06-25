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

package nfs4

import (
	"context"
	"fmt"
	"sync/atomic"
)

// openSeqid is a process-wide monotonically increasing seqid source for the
// single open-owner this connection uses. NFSv4.0 requires per-open-owner seqids
// to advance by one on each OPEN/CLOSE.
//
// A single shared owner keeps the v4.0 sequencing simple; richer per-owner
// tracking can be layered later.

// OpenResult bundles the outcome of an OPEN: the opened file's handle and the
// open stateid (already confirmed if the server required it).
type OpenResult struct {
	FH      FileHandle
	Stateid Stateid
}

// nextSeqid returns the next open-owner seqid.
func (c *Conn) nextSeqid() uint32 {
	return uint32(atomic.AddUint32(&c.openSeqid, 1))
}

// ownerID returns a stable open-owner identifier derived from the clientid.
func (c *Conn) ownerID() []byte {
	id := c.ClientID()
	return []byte(fmt.Sprintf("go-nfs-owner-%d", id))
}

// Open opens (and optionally creates) name within directory dir. share is the
// OPEN4_SHARE_ACCESS_* mask. If create is non-nil, the file is created with the
// given settable attributes (UNCHECKED mode); O_EXCL is requested when excl is
// true (GUARDED). It returns the opened file handle and confirmed stateid.
func (c *Conn) Open(ctx context.Context, dir FileHandle, name string, share uint32, createAttrMask Bitmap, createAttrVals []byte, doCreate, excl bool) (*OpenResult, error) {
	args := OpenArgs{
		Seqid:       c.nextSeqid(),
		ShareAccess: share,
		ShareDeny:   OpenShareDenyNone,
		ClientID:    c.ClientID(),
		Owner:       c.ownerID(),
		Name:        name,
	}
	if doCreate {
		args.OpenType = Open4Create
		if excl {
			args.CreateMode = CreateGuarded
		} else {
			args.CreateMode = CreateUnchecked
		}
		args.CreateAttrMask = createAttrMask
		args.CreateAttrVals = createAttrVals
	} else {
		args.OpenType = Open4NoCreate
	}

	var putRes StatusRes
	var openRes OpenRes
	var getRes GetfhRes
	comp := c.compound().
		Add(PutfhArgs{FH: dir}).
		Add(args).
		Add(GetfhArgs{})
	r, err := c.caller.CallCompound(ctx, comp, []Res{&putRes, &openRes, &getRes})
	if err != nil {
		return nil, fmt.Errorf("nfs4: OPEN: %w", err)
	}
	if err := r.Err(); err != nil {
		return nil, err
	}
	if openRes.Status != NFS4_OK {
		return nil, openRes.Status.Err()
	}

	res := &OpenResult{FH: FileHandle(getRes.FH), Stateid: openRes.Stateid}

	if openRes.NeedsConfirm() {
		confirmed, err := c.openConfirm(ctx, res.FH, openRes.Stateid)
		if err != nil {
			return nil, err
		}
		res.Stateid = confirmed
	}
	return res, nil
}

// openConfirm issues OPEN_CONFIRM and returns the confirmed stateid.
func (c *Conn) openConfirm(ctx context.Context, fh FileHandle, sid Stateid) (Stateid, error) {
	var putRes StatusRes
	var ocRes OpenConfirmRes
	comp := c.compound().
		Add(PutfhArgs{FH: fh}).
		Add(OpenConfirmArgs{Stateid: sid, Seqid: c.nextSeqid()})
	r, err := c.caller.CallCompound(ctx, comp, []Res{&putRes, &ocRes})
	if err != nil {
		return Stateid{}, fmt.Errorf("nfs4: OPEN_CONFIRM: %w", err)
	}
	if err := r.Err(); err != nil {
		return Stateid{}, err
	}
	if ocRes.Status != NFS4_OK {
		return Stateid{}, ocRes.Status.Err()
	}
	return ocRes.Stateid, nil
}

// Write writes data at offset to the file fh using the open stateid and the
// given stability level. It returns the bytes written and the achieved
// stability.
func (c *Conn) Write(ctx context.Context, fh FileHandle, sid Stateid, offset uint64, data []byte, stable StableHow) (uint32, StableHow, error) {
	var putRes StatusRes
	var wRes WriteRes
	comp := c.compound().
		Add(PutfhArgs{FH: fh}).
		Add(WriteArgs{Stateid: sid, Offset: offset, Stable: stable, Data: data})
	r, err := c.caller.CallCompound(ctx, comp, []Res{&putRes, &wRes})
	if err != nil {
		return 0, 0, fmt.Errorf("nfs4: WRITE: %w", err)
	}
	if err := r.Err(); err != nil {
		return 0, 0, err
	}
	if wRes.Status != NFS4_OK {
		return 0, 0, wRes.Status.Err()
	}
	return wRes.Count, wRes.Committed, nil
}

// Commit flushes cached writes in [offset, offset+count) to stable storage.
func (c *Conn) Commit(ctx context.Context, fh FileHandle, offset uint64, count uint32) error {
	var putRes StatusRes
	var cRes CommitRes
	comp := c.compound().
		Add(PutfhArgs{FH: fh}).
		Add(CommitArgs{Offset: offset, Count: count})
	r, err := c.caller.CallCompound(ctx, comp, []Res{&putRes, &cRes})
	if err != nil {
		return fmt.Errorf("nfs4: COMMIT: %w", err)
	}
	if err := r.Err(); err != nil {
		return err
	}
	return cRes.Status.Err()
}

// CloseFile closes an open file identified by fh and its open stateid. (The
// connection-level Close that tears down the transport lives in conn.go.)
func (c *Conn) CloseFile(ctx context.Context, fh FileHandle, sid Stateid) error {
	var putRes StatusRes
	var clRes CloseRes
	comp := c.compound().
		Add(PutfhArgs{FH: fh}).
		Add(CloseArgs{Seqid: c.nextSeqid(), Stateid: sid})
	r, err := c.caller.CallCompound(ctx, comp, []Res{&putRes, &clRes})
	if err != nil {
		return fmt.Errorf("nfs4: CLOSE: %w", err)
	}
	if err := r.Err(); err != nil {
		return err
	}
	return clRes.Status.Err()
}

// Mkdir creates a directory named name in dir with the given attributes and
// returns its filehandle.
func (c *Conn) Mkdir(ctx context.Context, dir FileHandle, name string, attrMask Bitmap, attrVals []byte) (FileHandle, error) {
	return c.create(ctx, dir, CreateArgs{Type: 2 /* NF4DIR */, ObjName: name, AttrMask: attrMask, AttrVals: attrVals})
}

// Symlink creates a symbolic link named name in dir pointing at target.
func (c *Conn) Symlink(ctx context.Context, dir FileHandle, name, target string, attrMask Bitmap, attrVals []byte) (FileHandle, error) {
	return c.create(ctx, dir, CreateArgs{Type: 5 /* NF4LNK */, LinkData: target, ObjName: name, AttrMask: attrMask, AttrVals: attrVals})
}

// create issues PUTFH(dir) + CREATE + GETFH and returns the new object's fh.
func (c *Conn) create(ctx context.Context, dir FileHandle, args CreateArgs) (FileHandle, error) {
	var putRes StatusRes
	var crRes CreateRes
	var getRes GetfhRes
	comp := c.compound().
		Add(PutfhArgs{FH: dir}).
		Add(args).
		Add(GetfhArgs{})
	r, err := c.caller.CallCompound(ctx, comp, []Res{&putRes, &crRes, &getRes})
	if err != nil {
		return nil, fmt.Errorf("nfs4: CREATE: %w", err)
	}
	if err := r.Err(); err != nil {
		return nil, err
	}
	if crRes.Status != NFS4_OK {
		return nil, crRes.Status.Err()
	}
	return FileHandle(getRes.FH), nil
}

// Remove deletes the entry name from directory dir.
func (c *Conn) Remove(ctx context.Context, dir FileHandle, name string) error {
	var putRes StatusRes
	var rmRes RemoveRes
	comp := c.compound().
		Add(PutfhArgs{FH: dir}).
		Add(RemoveArgs{Name: name})
	r, err := c.caller.CallCompound(ctx, comp, []Res{&putRes, &rmRes})
	if err != nil {
		return fmt.Errorf("nfs4: REMOVE: %w", err)
	}
	if err := r.Err(); err != nil {
		return err
	}
	return rmRes.Status.Err()
}

// Rename moves oldName in srcDir to newName in dstDir. It uses
// PUTFH(srcDir)+SAVEFH+PUTFH(dstDir)+RENAME.
func (c *Conn) Rename(ctx context.Context, srcDir FileHandle, oldName string, dstDir FileHandle, newName string) error {
	var p1, sv, p2 StatusRes
	var rnRes RenameRes
	comp := c.compound().
		Add(PutfhArgs{FH: srcDir}).
		Add(SavefhArgs{}).
		Add(PutfhArgs{FH: dstDir}).
		Add(RenameArgs{OldName: oldName, NewName: newName})
	r, err := c.caller.CallCompound(ctx, comp, []Res{&p1, &sv, &p2, &rnRes})
	if err != nil {
		return fmt.Errorf("nfs4: RENAME: %w", err)
	}
	if err := r.Err(); err != nil {
		return err
	}
	return rnRes.Status.Err()
}

// Link creates a hard link newName in dstDir pointing at the file src.
func (c *Conn) Link(ctx context.Context, src FileHandle, dstDir FileHandle, newName string) error {
	var p1, sv, p2 StatusRes
	var lnRes LinkRes
	comp := c.compound().
		Add(PutfhArgs{FH: src}).
		Add(SavefhArgs{}).
		Add(PutfhArgs{FH: dstDir}).
		Add(LinkArgs{NewName: newName})
	r, err := c.caller.CallCompound(ctx, comp, []Res{&p1, &sv, &p2, &lnRes})
	if err != nil {
		return fmt.Errorf("nfs4: LINK: %w", err)
	}
	if err := r.Err(); err != nil {
		return err
	}
	return lnRes.Status.Err()
}

// SetAttr sets attributes on fh using the given stateid (use AnonStateid for
// non-size changes; a SIZE change requires an open stateid).
func (c *Conn) SetAttr(ctx context.Context, fh FileHandle, sid Stateid, attrMask Bitmap, attrVals []byte) error {
	var putRes StatusRes
	var saRes SetattrRes
	comp := c.compound().
		Add(PutfhArgs{FH: fh}).
		Add(SetattrArgs{Stateid: sid, AttrMask: attrMask, AttrVals: attrVals})
	r, err := c.caller.CallCompound(ctx, comp, []Res{&putRes, &saRes})
	if err != nil {
		return fmt.Errorf("nfs4: SETATTR: %w", err)
	}
	if err := r.Err(); err != nil {
		return err
	}
	return saRes.Status.Err()
}
