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

package rpc

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/mactav683/go-nfs-client/xdr"
)

// NFS program/version constants for the NULL ping.
const (
	nfsProgram  = 100003
	nfsVersion  = 4
	nfsProcNull = 0
)

// serverAddr returns the integration NFS server address, defaulting to the
// dockerized server's mapped port.
func serverAddr() string {
	if a := os.Getenv("NFS_SERVER_ADDR"); a != "" {
		return a
	}
	return "127.0.0.1:2049"
}

// TestRPCNullPing issues a real RPC NULL (procedure 0) to a running NFSv4
// server and expects an accepted SUCCESS reply with an empty result body.
func TestRPCNullPing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cli, err := Dial(ctx, serverAddr(), ClientOptions{
		Cred: AuthNull(),
		Verf: AuthNull(),
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer cli.Close()

	err = cli.Call(ctx, nfsProgram, nfsVersion, nfsProcNull,
		func(e *xdr.Encoder) {}, // NULL takes no arguments
		func(d *xdr.Decoder) {}, // NULL returns no results
	)
	if err != nil {
		t.Fatalf("RPC NULL ping failed: %v", err)
	}
}
