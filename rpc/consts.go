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

// RPC protocol version (RFC 5531 §9). Only version 2 exists.
const RPCVersion = 2

// msg_type discriminants (RFC 5531 §9).
const (
	msgCall  uint32 = 0
	msgReply uint32 = 1
)

// reply_stat: top-level reply discriminant.
const (
	MsgAccepted uint32 = 0
	MsgDenied   uint32 = 1
)

// accept_stat: the status within an accepted reply.
const (
	AcceptSuccess      uint32 = 0
	AcceptProgUnavail  uint32 = 1
	AcceptProgMismatch uint32 = 2
	AcceptProcUnavail  uint32 = 3
	AcceptGarbageArgs  uint32 = 4
	AcceptSystemErr    uint32 = 5
)

// reject_stat: the reason within a denied reply.
const (
	RejectRPCMismatch uint32 = 0
	RejectAuthError   uint32 = 1
)

// auth_flavor values (RFC 5531 §8).
const (
	AuthFlavorNull  uint32 = 0
	AuthFlavorSys   uint32 = 1
	AuthFlavorShort uint32 = 2
	AuthFlavorGSS   uint32 = 6
)
