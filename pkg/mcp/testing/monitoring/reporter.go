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
	"sync"

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
	mutex             *sync.Mutex
}

// SetClientsTotal updates the current client count to the given argument.
func (s *InMemoryServerStatsContext) SetClientsTotal(clients int64) {
	s.mutex.Lock()
	s.ClientsTotal = clients
	s.mutex.Unlock()
}

// RecordSendError records an error during a network send with its error
// string and code.
func (s *InMemoryServerStatsContext) RecordSendError(err error, code codes.Code) {
	s.mutex.Lock()
	s.SendFailuresTotal[errorCodeKey{err.Error(), code}]++
	s.mutex.Unlock()
}

// RecordRecvError records an error during a network recv with its error
// string and code.
func (s *InMemoryServerStatsContext) RecordRecvError(err error, code codes.Code) {
	s.mutex.Lock()
	s.RecvFailuresTotal[errorCodeKey{err.Error(), code}]++
	s.mutex.Unlock()
}

// RecordRequestSize records the size of a request from a connection for a specific type URL.
func (s *InMemoryServerStatsContext) RecordRequestSize(typeURL string, connectionID int64, size int) {
	key := requestKey{typeURL, connectionID}
	s.mutex.Lock()
	s.RequestSizesBytes[key] = append(s.RequestSizesBytes[key], int64(size))
	s.mutex.Unlock()
}

// RecordRequestAck records an ACK message for a type URL on a connection.
func (s *InMemoryServerStatsContext) RecordRequestAck(typeURL string, connectionID int64) {
	s.mutex.Lock()
	s.RequestAcksTotal[requestKey{typeURL, connectionID}]++
	s.mutex.Unlock()
}

// RecordRequestNack records a NACK message for a type URL on a connection.
func (s *InMemoryServerStatsContext) RecordRequestNack(typeURL string, connectionID int64) {
	s.mutex.Lock()
	s.RequestNacksTotal[requestKey{typeURL, connectionID}]++
	s.mutex.Unlock()
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
		mutex:             &sync.Mutex{},
	}
}

type nackKey struct {
	error   string
	typeURL string
}

type InMemoryClientStatsContext struct {
	RequestAcksTotal   map[string]int64
	RequestNacksTotal  map[nackKey]int64
	SendFailuresTotal  map[errorCodeKey]int64
	RecvFailuresTotal  map[errorCodeKey]int64
	ReconnectionsTotal int64
	mutex              *sync.Mutex
}

// RecordSendError records an error during a network send with its error
// string and code.
func (s *InMemoryClientStatsContext) RecordSendError(err error, code codes.Code) {
	s.mutex.Lock()
	s.RecvFailuresTotal[errorCodeKey{err.Error(), code}]++
	s.mutex.Unlock()
}

// RecordRecvError records an error during a network recv with its error
// string and code.
func (s *InMemoryClientStatsContext) RecordRecvError(err error, code codes.Code) {
	s.mutex.Lock()
	s.RecvFailuresTotal[errorCodeKey{err.Error(), code}]++
	s.mutex.Unlock()
}

// RecordRequestAck records an ACK message for a type URL on a connection.
func (s *InMemoryClientStatsContext) RecordRequestAck(typeURL string) {
	s.mutex.Lock()
	s.RequestAcksTotal[typeURL]++
	s.mutex.Unlock()
}

// RecordRequestNack records a NACK message for a type URL on a connection.
func (s *InMemoryClientStatsContext) RecordRequestNack(typeURL string, err error) {
	s.mutex.Lock()
	s.RequestNacksTotal[nackKey{err.Error(), typeURL}]++
	s.mutex.Unlock()
}

func (s *InMemoryClientStatsContext) RecordReconnect() {
	s.mutex.Lock()
	s.ReconnectionsTotal++
	s.mutex.Unlock()
}

// NewInMemoryClientStatsContext creates a new context for tracking metrics
// in memory.
func NewInMemoryClientStatsContext() *InMemoryClientStatsContext {
	return &InMemoryClientStatsContext{
		RequestAcksTotal:  make(map[string]int64),
		RequestNacksTotal: make(map[nackKey]int64),
		SendFailuresTotal: make(map[errorCodeKey]int64),
		RecvFailuresTotal: make(map[errorCodeKey]int64),
		mutex:             &sync.Mutex{},
	}
}
