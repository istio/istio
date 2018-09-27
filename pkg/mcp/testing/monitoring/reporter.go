// Copyright 2018 Istio Authors
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

package mcptestmon

import (
	"google.golang.org/grpc/codes"
)

type errorCodeKey struct {
	error string
	code  codes.Code
}

type requestKey struct {
	typeURL      string
	connectionID int64
}

// InMemoryServerStatsContext enables MCP server metric collection which is
// stored in memory for testing purposes.
type InMemoryServerStatsContext struct {
	ClientsTotal      int64
	RequestSizesBytes map[requestKey][]int64
	RequestAcksTotal  map[requestKey]int64
	RequestNacksTotal map[requestKey]int64
	SendFailuresTotal map[errorCodeKey]int64
	RecvFailuresTotal map[errorCodeKey]int64
}

// SetClientsTotal updates the current client count to the given argument.
func (s *InMemoryServerStatsContext) SetClientsTotal(clients int64) {
	s.ClientsTotal = clients
}

// RecordSendError records an error during a network send with its error
// string and code.
func (s *InMemoryServerStatsContext) RecordSendError(err error, code codes.Code) {
	s.SendFailuresTotal[errorCodeKey{err.Error(), code}]++
}

// RecordRecvError records an error during a network recv with its error
// string and code.
func (s *InMemoryServerStatsContext) RecordRecvError(err error, code codes.Code) {
	s.RecvFailuresTotal[errorCodeKey{err.Error(), code}]++
}

// RecordRequestSize records the size of a request from a connection for a specific type URL.
func (s *InMemoryServerStatsContext) RecordRequestSize(typeURL string, connectionID int64, size int) {
	key := requestKey{typeURL, connectionID}
	s.RequestSizesBytes[key] = append(s.RequestSizesBytes[key], int64(size))
}

// RecordRequestAck records an ACK message for a type URL on a connection.
func (s *InMemoryServerStatsContext) RecordRequestAck(typeURL string, connectionID int64) {
	s.RequestAcksTotal[requestKey{typeURL, connectionID}]++
}

// RecordRequestNack records a NACK message for a type URL on a connection.
func (s *InMemoryServerStatsContext) RecordRequestNack(typeURL string, connectionID int64) {
	s.RequestNacksTotal[requestKey{typeURL, connectionID}]++
}

// NewInMemoryServerStatsContext creates a new context for tracking metrics
// in memory.
func NewInMemoryServerStatsContext() *InMemoryServerStatsContext {
	return &InMemoryServerStatsContext{
		RequestSizesBytes: make(map[requestKey][]int64),
		RequestAcksTotal:  make(map[requestKey]int64),
		RequestNacksTotal: make(map[requestKey]int64),
		SendFailuresTotal: make(map[errorCodeKey]int64),
		RecvFailuresTotal: make(map[errorCodeKey]int64),
	}
}
