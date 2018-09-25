package mcptest

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

type InMemoryStatsContext struct {
	ClientsTotal      int64
	RequestSizesBytes map[requestKey][]int64
	RequestAcksTotal  map[requestKey]int64
	RequestNacksTotal map[requestKey]int64
	SendFailuresTotal map[errorCodeKey]int64
	RecvFailuresTotal map[errorCodeKey]int64
}

func (s *InMemoryStatsContext) SetClientsTotal(clients int64) {
	s.ClientsTotal = clients
}

func (s *InMemoryStatsContext) RecordSendError(err error, code codes.Code) {
	s.SendFailuresTotal[errorCodeKey{err.Error(), code}]++
}

func (s *InMemoryStatsContext) RecordRecvError(err error, code codes.Code) {
	s.RecvFailuresTotal[errorCodeKey{err.Error(), code}]++
}

func (s *InMemoryStatsContext) RecordRequestSize(typeURL string, connectionID int64, size int) {
	key := requestKey{typeURL, connectionID}
	s.RequestSizesBytes[key] = append(s.RequestSizesBytes[key], int64(size))
}

func (s *InMemoryStatsContext) RecordRequestAck(typeURL string, connectionID int64) {
	s.RequestAcksTotal[requestKey{typeURL, connectionID}]++
}

func (s *InMemoryStatsContext) RecordRequestNack(typeURL string, connectionID int64) {
	s.RequestNacksTotal[requestKey{typeURL, connectionID}]++
}

func NewInMemoryReporter() *InMemoryStatsContext {
	return &InMemoryStatsContext{
		RequestSizesBytes: make(map[requestKey][]int64),
		RequestAcksTotal:  make(map[requestKey]int64),
		RequestNacksTotal: make(map[requestKey]int64),
		SendFailuresTotal: make(map[errorCodeKey]int64),
		RecvFailuresTotal: make(map[errorCodeKey]int64),
	}
}
