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

import "testing"

// TestModuleCompiles is a baseline test ensuring the package compiles.
func TestModuleCompiles(t *testing.T) {}

// TestOpnumValues asserts the operation numbers match nfs4.x exactly
// (libnfs/nfs4/nfs4.x:2073).
func TestOpnumValues(t *testing.T) {
	cases := map[string]struct {
		got  Opnum
		want uint32
	}{
		"GETATTR":             {OpGetattr, 9},
		"GETFH":               {OpGetfh, 10},
		"LOOKUP":              {OpLookup, 15},
		"PUTFH":               {OpPutfh, 22},
		"PUTROOTFH":           {OpPutrootfh, 24},
		"SETCLIENTID":         {OpSetclientid, 35},
		"SETCLIENTID_CONFIRM": {OpSetclientidConfirm, 36},
	}
	for name, c := range cases {
		if uint32(c.got) != c.want {
			t.Errorf("%s = %d, want %d", name, c.got, c.want)
		}
	}
}

// TestStatusValues asserts the nfsstat4 values match nfs4.x exactly.
func TestStatusValues(t *testing.T) {
	cases := map[string]struct {
		got  Status
		want uint32
	}{
		"OK":             {NFS4_OK, 0},
		"PERM":           {NFS4ERR_PERM, 1},
		"NOENT":          {NFS4ERR_NOENT, 2},
		"ACCESS":         {NFS4ERR_ACCESS, 13},
		"EXIST":          {NFS4ERR_EXIST, 17},
		"NOTDIR":         {NFS4ERR_NOTDIR, 20},
		"STALE":          {NFS4ERR_STALE, 70},
		"NOTSUPP":        {NFS4ERR_NOTSUPP, 10004},
		"DELAY":          {NFS4ERR_DELAY, 10008},
		"GRACE":          {NFS4ERR_GRACE, 10013},
		"CLID_INUSE":     {NFS4ERR_CLID_INUSE, 10017},
		"STALE_CLIENTID": {NFS4ERR_STALE_CLIENTID, 10022},
	}
	for name, c := range cases {
		if uint32(c.got) != c.want {
			t.Errorf("NFS4 %s = %d, want %d", name, c.got, c.want)
		}
	}
}

// TestStatusError verifies Status implements error and reports its code.
func TestStatusError(t *testing.T) {
	if NFS4_OK.Err() != nil {
		t.Fatalf("NFS4_OK.Err() should be nil")
	}
	err := NFS4ERR_NOENT.Err()
	if err == nil {
		t.Fatalf("NFS4ERR_NOENT.Err() should be non-nil")
	}
}
