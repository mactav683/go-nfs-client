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

package rpc

import (
	"bytes"

	"github.com/mactav683/go-nfs-client/xdr"
)

// OpaqueAuth is the RFC 5531 §8.2 opaque_auth structure: an authentication
// flavor and an opaque body whose meaning depends on the flavor.
type OpaqueAuth struct {
	Flavor uint32
	Body   []byte
}

// Encode writes the opaque_auth as flavor (uint32) followed by the body as a
// variable-length opaque.
func (a OpaqueAuth) Encode(e *xdr.Encoder) {
	e.Uint32(a.Flavor)
	e.Opaque(a.Body)
}

// DecodeOpaqueAuth reads an opaque_auth from d.
func DecodeOpaqueAuth(d *xdr.Decoder) OpaqueAuth {
	var a OpaqueAuth
	a.Flavor = d.Uint32()
	a.Body = d.Opaque()
	return a
}

// AuthNull returns the AUTH_NULL credential/verifier (flavor 0, empty body).
func AuthNull() OpaqueAuth {
	return OpaqueAuth{Flavor: AuthFlavorNull, Body: nil}
}

// AuthSys is the AUTH_SYS (a.k.a. AUTH_UNIX) credential body defined in
// RFC 5531 §8.2: a timestamp, the client machine name, the effective uid and
// gid, and a list of supplementary group ids.
type AuthSys struct {
	Stamp       uint32
	MachineName string
	UID         uint32
	GID         uint32
	GIDs        []uint32
}

// body serializes the AUTH_SYS body (without the outer opaque_auth wrapper).
func (c AuthSys) body() []byte {
	var buf bytes.Buffer
	e := xdr.NewEncoder(&buf)
	e.Uint32(c.Stamp)
	e.String(c.MachineName)
	e.Uint32(c.UID)
	e.Uint32(c.GID)
	e.Uint32(uint32(len(c.GIDs)))
	for _, g := range c.GIDs {
		e.Uint32(g)
	}
	return buf.Bytes()
}

// OpaqueAuth wraps the AUTH_SYS body in an opaque_auth with flavor AUTH_SYS.
func (c AuthSys) OpaqueAuth() OpaqueAuth {
	return OpaqueAuth{Flavor: AuthFlavorSys, Body: c.body()}
}

// Encode writes the AUTH_SYS credential as a complete opaque_auth.
func (c AuthSys) Encode(e *xdr.Encoder) {
	c.OpaqueAuth().Encode(e)
}
