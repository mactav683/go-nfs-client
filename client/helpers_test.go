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
	"io"
	"io/fs"
	"testing/fstest"

	"github.com/mactav683/go-nfs-client/xdr"
)

// attrEncoder is a thin wrapper over xdr.Encoder for building fake attribute
// payloads in tests with terse method names.
type attrEncoder struct {
	e *xdr.Encoder
}

func newAttrEncoder(w io.Writer) *attrEncoder {
	return &attrEncoder{e: xdr.NewEncoder(w)}
}

func (a *attrEncoder) u32(v uint32) { a.e.Uint32(v) }
func (a *attrEncoder) u64(v uint64) { a.e.Uint64(v) }
func (a *attrEncoder) i64(v int64)  { a.e.Int64(v) }
func (a *attrEncoder) str(s string) { a.e.String(s) }

// testFS runs the standard library's fs.FS conformance harness.
func testFS(fsys fs.FS, expected ...string) error {
	return fstest.TestFS(fsys, expected...)
}
