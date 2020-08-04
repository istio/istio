// Copyright Istio Authors
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

package monitoring

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

type nackKey struct {
	typeURL      string
	connectionID int64
	code         codes.Code
}

// InMemoryStatsContext enables MCP server metric collection which is
// stored in memory for testing purposes.
type InMemoryStatsContext struct {
	mutex                    sync.Mutex
	StreamTotal              int64
	RequestSizesBytes        map[requestKey][]int64
	RequestAcksTotal         map[requestKey]int64
	RequestNacksTotal        map[nackKey]int64
	SendFailuresTotal        map[errorCodeKey]int64
	RecvFailuresTotal        map[errorCodeKey]int64
	StreamCreateSuccessTotal int64
}

// SetStreamCount updates the current stream count to the given argument.
func (s *InMemoryStatsContext) SetStreamCount(clients int64) {
	s.mutex.Lock()
	s.StreamTotal = clients
	s.mutex.Unlock()
}

// RecordSendError records an error during a network send with its error
// string and code.
func (s *InMemoryStatsContext) RecordSendError(err error, code codes.Code) {
	s.mutex.Lock()
	s.SendFailuresTotal[errorCodeKey{err.Error(), code}]++
	s.mutex.Unlock()
}

// RecordRecvError records an error during a network recv with its error
// string and code.
func (s *InMemoryStatsContext) RecordRecvError(err error, code codes.Code) {
	s.mutex.Lock()
	s.RecvFailuresTotal[errorCodeKey{err.Error(), code}]++
	s.mutex.Unlock()
}

// RecordRequestSize records the size of a request from a connection for a specific type URL.
func (s *InMemoryStatsContext) RecordRequestSize(typeURL string, connectionID int64, size int) {
	key := requestKey{typeURL, connectionID}
	s.mutex.Lock()
	s.RequestSizesBytes[key] = append(s.RequestSizesBytes[key], int64(size))
	s.mutex.Unlock()
}

// RecordRequestAck records an ACK message for a type URL on a connection.
func (s *InMemoryStatsContext) RecordRequestAck(typeURL string, connectionID int64) {
	s.mutex.Lock()
	s.RequestAcksTotal[requestKey{typeURL, connectionID}]++
	s.mutex.Unlock()
}

// RecordRequestNack records a NACK message for a type URL on a connection.
func (s *InMemoryStatsContext) RecordRequestNack(typeURL string, connectionID int64, code codes.Code) {
	s.mutex.Lock()
	s.RequestNacksTotal[nackKey{typeURL, connectionID, code}]++
	s.mutex.Unlock()
}

// RecordStreamCreateSuccess records a successful stream connection.
func (s *InMemoryStatsContext) RecordStreamCreateSuccess() {
	s.mutex.Lock()
	s.StreamCreateSuccessTotal++
	s.mutex.Unlock()
}

// Close implements io.Closer.
func (s *InMemoryStatsContext) Close() error {
	return nil
}

// NewInMemoryStatsContext creates a new context for tracking metrics
// in memory.
func NewInMemoryStatsContext() *InMemoryStatsContext {
	return &InMemoryStatsContext{
		RequestSizesBytes: make(map[requestKey][]int64),
		RequestAcksTotal:  make(map[requestKey]int64),
		RequestNacksTotal: make(map[nackKey]int64),
		SendFailuresTotal: make(map[errorCodeKey]int64),
		RecvFailuresTotal: make(map[errorCodeKey]int64),
	}
}
