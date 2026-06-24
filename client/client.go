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
	"context"
	"strings"

	"github.com/mactav683/go-nfs-client/nfs4"
	"github.com/mactav683/go-nfs-client/rpc"
)

// Compile-time check that *nfs4.Conn satisfies the read Protocol.
var _ Protocol = (*nfs4.Conn)(nil)

// Client is a mounted NFSv4 connection exposing filesystem views.
type Client struct {
	conn *nfs4.Conn
}

// Mount dials server (host:port), mounts the given export path with the
// supplied RPC credential, and returns a Client. The export path is resolved
// from the server pseudo-root via successive LOOKUPs.
func Mount(ctx context.Context, server, export string, cred rpc.OpaqueAuth) (*Client, error) {
	conn, err := nfs4.Dial(ctx, server, cred, nfs4.ConnConfig{})
	if err != nil {
		return nil, err
	}
	// Resolve the export path so the returned FS is rooted at the export.
	if export != "" && export != "/" {
		comps := strings.Split(strings.Trim(export, "/"), "/")
		if _, err := conn.LookupPath(ctx, comps); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	return &Client{conn: conn}, nil
}

// Close releases the underlying connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// FS returns a read-only io/fs.FS view rooted at the server pseudo-root. Paths
// are resolved relative to it.
//
// Note: rooting the FS at a specific export (rather than the pseudo-root) is a
// convenience added with the write path; for now callers pass export-relative
// paths through the pseudo-root.
func (c *Client) FS(ctx context.Context) *FS {
	return New(c.conn).WithContext(ctx)
}

// Conn exposes the underlying protocol connection for advanced use.
func (c *Client) Conn() *nfs4.Conn {
	return c.conn
}
