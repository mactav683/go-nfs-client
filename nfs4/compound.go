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

	"github.com/mactav683/go-nfs-client/xdr"
)

// Compound is a builder for a COMPOUND4args request. Operations are appended in
// order and encoded as the nfs_argop4 argarray. The same Compound carries the
// tag and minor version negotiated for the request.
type Compound struct {
	Tag          string
	MinorVersion uint32
	ops          []Arg
}

// NewCompound returns a Compound with the given tag. The tag is a free-form
// label echoed by the server and is typically empty.
func NewCompound(tag string) *Compound {
	return &Compound{Tag: tag}
}

// Add appends an operation to the compound and returns the builder for
// chaining.
func (c *Compound) Add(op Arg) *Compound {
	c.ops = append(c.ops, op)
	return c
}

// Ops returns the operations accumulated so far.
func (c *Compound) Ops() []Arg {
	return c.ops
}

// Encode writes the COMPOUND4args: tag, minorversion, and the argarray of
// nfs_argop4 unions.
func (c *Compound) Encode(e *xdr.Encoder) {
	e.String(c.Tag)
	e.Uint32(c.MinorVersion)
	e.Uint32(uint32(len(c.ops)))
	for _, op := range c.ops {
		op.EncodeArg(e)
	}
}

// CompoundResult holds the decoded outcome of a COMPOUND4res.
type CompoundResult struct {
	// Status is the overall compound status (the status of the last op the
	// server processed, or NFS4_OK if all succeeded).
	Status Status
	// Tag echoes the server's response tag.
	Tag string
	// NumResults is the number of resops the server returned. Because the
	// server stops at the first failing op, this may be fewer than the number
	// of ops sent.
	NumResults int
}

// Err returns a non-nil error if the overall compound status is not NFS4_OK.
func (r *CompoundResult) Err() error {
	return r.Status.Err()
}

// DecodeCompound decodes a COMPOUND4res from d. results must be a slice of Res
// decoders in the same order as the ops that were sent; DecodeCompound invokes
// each result decoder for the corresponding returned resop. Because servers
// stop processing at the first failing op (early termination), fewer resops
// than results may be present — the trailing result decoders are left
// untouched.
//
// Each returned resop carries its opnum discriminant, which is validated
// against the expected op order; a mismatch is reported as an error.
func DecodeCompound(d *xdr.Decoder, results []Res) (*CompoundResult, error) {
	r := &CompoundResult{}
	r.Status = Status(d.Uint32())
	r.Tag = d.String()
	count := d.Uint32()
	if err := d.Err(); err != nil {
		return nil, fmt.Errorf("nfs4: decoding compound header: %w", err)
	}

	if int(count) > len(results) {
		return nil, fmt.Errorf("nfs4: server returned %d resops but only %d expected", count, len(results))
	}

	for i := 0; i < int(count); i++ {
		opnum := Opnum(d.Uint32())
		if err := d.Err(); err != nil {
			return nil, fmt.Errorf("nfs4: decoding resop[%d] opnum: %w", i, err)
		}
		if got := expectedOp(results[i]); got != 0 && got != opnum {
			return nil, fmt.Errorf("nfs4: resop[%d] opnum %d does not match expected %d", i, opnum, got)
		}
		results[i].DecodeRes(d)
		if err := d.Err(); err != nil {
			return nil, fmt.Errorf("nfs4: decoding resop[%d] body: %w", i, err)
		}
	}
	r.NumResults = int(count)
	return r, nil
}

// expectedOp returns the opnum a result decoder corresponds to, if it
// advertises one via the OpResult interface; otherwise 0 (no check).
func expectedOp(res Res) Opnum {
	if or, ok := res.(interface{ Op() Opnum }); ok {
		return or.Op()
	}
	return 0
}
