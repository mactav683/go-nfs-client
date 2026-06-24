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
	"fmt"
	"io/fs"
)

// NFS program and version numbers (RFC 7530).
const (
	Program      = 100003
	Version4     = 4
	ProcNull     = 0
	ProcCompound = 1
)

// Protocol size constants from nfs4.x.
const (
	FHSize       = 128 // NFS4_FHSIZE
	VerifierSize = 8   // NFS4_VERIFIER_SIZE
	OpaqueLimit  = 1024
)

// Opnum is the nfs_opnum4 operation number (nfs4.x:2073).
type Opnum uint32

// Operation numbers used by the client. Values match nfs4.x exactly.
const (
	OpAccess             Opnum = 3
	OpClose              Opnum = 4
	OpCommit             Opnum = 5
	OpCreate             Opnum = 6
	OpDelegpurge         Opnum = 7
	OpDelegreturn        Opnum = 8
	OpGetattr            Opnum = 9
	OpGetfh              Opnum = 10
	OpLink               Opnum = 11
	OpLock               Opnum = 12
	OpLockt              Opnum = 13
	OpLocku              Opnum = 14
	OpLookup             Opnum = 15
	OpLookupp            Opnum = 16
	OpNverify            Opnum = 17
	OpOpen               Opnum = 18
	OpOpenattr           Opnum = 19
	OpOpenConfirm        Opnum = 20
	OpOpenDowngrade      Opnum = 21
	OpPutfh              Opnum = 22
	OpPutpubfh           Opnum = 23
	OpPutrootfh          Opnum = 24
	OpRead               Opnum = 25
	OpReaddir            Opnum = 26
	OpReadlink           Opnum = 27
	OpRemove             Opnum = 28
	OpRename             Opnum = 29
	OpRenew              Opnum = 30
	OpRestorefh          Opnum = 31
	OpSavefh             Opnum = 32
	OpSecinfo            Opnum = 33
	OpSetattr            Opnum = 34
	OpSetclientid        Opnum = 35
	OpSetclientidConfirm Opnum = 36
	OpVerify             Opnum = 37
	OpWrite              Opnum = 38
	OpReleaseLockowner   Opnum = 39
	// NFSv4.1 operations (layered later).
	OpBindConnToSession Opnum = 41
	OpExchangeID        Opnum = 42
	OpCreateSession     Opnum = 43
	OpDestroySession    Opnum = 44
	OpSequence          Opnum = 53
	OpDestroyClientID   Opnum = 57
	OpReclaimComplete   Opnum = 58
	OpIllegal           Opnum = 10044
)

// Status is the nfsstat4 result code (nfs4.x:43).
type Status uint32

// nfsstat4 values. Values match nfs4.x exactly.
const (
	NFS4_OK                     Status = 0
	NFS4ERR_PERM                Status = 1
	NFS4ERR_NOENT               Status = 2
	NFS4ERR_IO                  Status = 5
	NFS4ERR_NXIO                Status = 6
	NFS4ERR_ACCESS              Status = 13
	NFS4ERR_EXIST               Status = 17
	NFS4ERR_XDEV                Status = 18
	NFS4ERR_NOTDIR              Status = 20
	NFS4ERR_ISDIR               Status = 21
	NFS4ERR_INVAL               Status = 22
	NFS4ERR_FBIG                Status = 27
	NFS4ERR_NOSPC               Status = 28
	NFS4ERR_ROFS                Status = 30
	NFS4ERR_MLINK               Status = 31
	NFS4ERR_NAMETOOLONG         Status = 63
	NFS4ERR_NOTEMPTY            Status = 66
	NFS4ERR_DQUOT               Status = 69
	NFS4ERR_STALE               Status = 70
	NFS4ERR_BADHANDLE           Status = 10001
	NFS4ERR_BAD_COOKIE          Status = 10003
	NFS4ERR_NOTSUPP             Status = 10004
	NFS4ERR_TOOSMALL            Status = 10005
	NFS4ERR_SERVERFAULT         Status = 10006
	NFS4ERR_BADTYPE             Status = 10007
	NFS4ERR_DELAY               Status = 10008
	NFS4ERR_SAME                Status = 10009
	NFS4ERR_DENIED              Status = 10010
	NFS4ERR_EXPIRED             Status = 10011
	NFS4ERR_LOCKED              Status = 10012
	NFS4ERR_GRACE               Status = 10013
	NFS4ERR_FHEXPIRED           Status = 10014
	NFS4ERR_WRONGSEC            Status = 10016
	NFS4ERR_CLID_INUSE          Status = 10017
	NFS4ERR_RESOURCE            Status = 10018
	NFS4ERR_MOVED               Status = 10019
	NFS4ERR_NOFILEHANDLE        Status = 10020
	NFS4ERR_MINOR_VERS_MISMATCH Status = 10021
	NFS4ERR_STALE_CLIENTID      Status = 10022
	NFS4ERR_STALE_STATEID       Status = 10023
	NFS4ERR_OLD_STATEID         Status = 10024
	NFS4ERR_BAD_STATEID         Status = 10025
	NFS4ERR_BAD_SEQID           Status = 10026
	NFS4ERR_NOT_SAME            Status = 10027
	NFS4ERR_LOCK_RANGE          Status = 10028
	NFS4ERR_SYMLINK             Status = 10029
	NFS4ERR_RESTOREFH           Status = 10030
	NFS4ERR_BADXDR              Status = 10036
	NFS4ERR_OPENMODE            Status = 10038
	NFS4ERR_BADOWNER            Status = 10039
	NFS4ERR_LOCKS_HELD          Status = 10037
	NFS4ERR_RECLAIM_BAD         Status = 10034
)

// String returns the symbolic name for known status codes.
func (s Status) String() string {
	if name, ok := statusNames[s]; ok {
		return name
	}
	return fmt.Sprintf("NFS4ERR(%d)", uint32(s))
}

// Err returns nil for NFS4_OK, otherwise a *StatusError carrying the code.
func (s Status) Err() error {
	if s == NFS4_OK {
		return nil
	}
	return &StatusError{Status: s}
}

// StatusError wraps a non-OK nfsstat4 as a Go error. It maps the common status
// codes onto the io/fs sentinel errors via Unwrap so that errors.Is(err,
// fs.ErrNotExist) and friends hold for the corresponding NFS codes.
type StatusError struct {
	Status Status
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("nfs4: %s", e.Status)
}

// Is allows errors.Is(err, fs.ErrNotExist) etc. to match by status code.
func (e *StatusError) Is(target error) bool {
	switch target {
	case fs.ErrNotExist:
		return e.Status == NFS4ERR_NOENT || e.Status == NFS4ERR_STALE
	case fs.ErrExist:
		return e.Status == NFS4ERR_EXIST
	case fs.ErrPermission:
		return e.Status == NFS4ERR_PERM || e.Status == NFS4ERR_ACCESS
	case fs.ErrInvalid:
		return e.Status == NFS4ERR_INVAL
	}
	return false
}

var statusNames = map[Status]string{
	NFS4_OK:                     "NFS4_OK",
	NFS4ERR_PERM:                "NFS4ERR_PERM",
	NFS4ERR_NOENT:               "NFS4ERR_NOENT",
	NFS4ERR_IO:                  "NFS4ERR_IO",
	NFS4ERR_NXIO:                "NFS4ERR_NXIO",
	NFS4ERR_ACCESS:              "NFS4ERR_ACCESS",
	NFS4ERR_EXIST:               "NFS4ERR_EXIST",
	NFS4ERR_XDEV:                "NFS4ERR_XDEV",
	NFS4ERR_NOTDIR:              "NFS4ERR_NOTDIR",
	NFS4ERR_ISDIR:               "NFS4ERR_ISDIR",
	NFS4ERR_INVAL:               "NFS4ERR_INVAL",
	NFS4ERR_FBIG:                "NFS4ERR_FBIG",
	NFS4ERR_NOSPC:               "NFS4ERR_NOSPC",
	NFS4ERR_ROFS:                "NFS4ERR_ROFS",
	NFS4ERR_MLINK:               "NFS4ERR_MLINK",
	NFS4ERR_NAMETOOLONG:         "NFS4ERR_NAMETOOLONG",
	NFS4ERR_NOTEMPTY:            "NFS4ERR_NOTEMPTY",
	NFS4ERR_DQUOT:               "NFS4ERR_DQUOT",
	NFS4ERR_STALE:               "NFS4ERR_STALE",
	NFS4ERR_BADHANDLE:           "NFS4ERR_BADHANDLE",
	NFS4ERR_BAD_COOKIE:          "NFS4ERR_BAD_COOKIE",
	NFS4ERR_NOTSUPP:             "NFS4ERR_NOTSUPP",
	NFS4ERR_TOOSMALL:            "NFS4ERR_TOOSMALL",
	NFS4ERR_SERVERFAULT:         "NFS4ERR_SERVERFAULT",
	NFS4ERR_BADTYPE:             "NFS4ERR_BADTYPE",
	NFS4ERR_DELAY:               "NFS4ERR_DELAY",
	NFS4ERR_SAME:                "NFS4ERR_SAME",
	NFS4ERR_DENIED:              "NFS4ERR_DENIED",
	NFS4ERR_EXPIRED:             "NFS4ERR_EXPIRED",
	NFS4ERR_LOCKED:              "NFS4ERR_LOCKED",
	NFS4ERR_GRACE:               "NFS4ERR_GRACE",
	NFS4ERR_FHEXPIRED:           "NFS4ERR_FHEXPIRED",
	NFS4ERR_WRONGSEC:            "NFS4ERR_WRONGSEC",
	NFS4ERR_CLID_INUSE:          "NFS4ERR_CLID_INUSE",
	NFS4ERR_RESOURCE:            "NFS4ERR_RESOURCE",
	NFS4ERR_MOVED:               "NFS4ERR_MOVED",
	NFS4ERR_NOFILEHANDLE:        "NFS4ERR_NOFILEHANDLE",
	NFS4ERR_MINOR_VERS_MISMATCH: "NFS4ERR_MINOR_VERS_MISMATCH",
	NFS4ERR_STALE_CLIENTID:      "NFS4ERR_STALE_CLIENTID",
	NFS4ERR_STALE_STATEID:       "NFS4ERR_STALE_STATEID",
	NFS4ERR_OLD_STATEID:         "NFS4ERR_OLD_STATEID",
	NFS4ERR_BAD_STATEID:         "NFS4ERR_BAD_STATEID",
	NFS4ERR_BAD_SEQID:           "NFS4ERR_BAD_SEQID",
	NFS4ERR_NOT_SAME:            "NFS4ERR_NOT_SAME",
	NFS4ERR_LOCK_RANGE:          "NFS4ERR_LOCK_RANGE",
	NFS4ERR_SYMLINK:             "NFS4ERR_SYMLINK",
	NFS4ERR_RESTOREFH:           "NFS4ERR_RESTOREFH",
	NFS4ERR_BADXDR:              "NFS4ERR_BADXDR",
	NFS4ERR_OPENMODE:            "NFS4ERR_OPENMODE",
	NFS4ERR_BADOWNER:            "NFS4ERR_BADOWNER",
	NFS4ERR_LOCKS_HELD:          "NFS4ERR_LOCKS_HELD",
	NFS4ERR_RECLAIM_BAD:         "NFS4ERR_RECLAIM_BAD",
}
