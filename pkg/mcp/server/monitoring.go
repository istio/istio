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

package server

import (
	"context"
	"strconv"
	"strings"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"google.golang.org/grpc/codes"
)

const (
	typeURL      = "typeURL"
	errorCode    = "code"
	errorStr     = "error"
	connectionID = "connectionID"
)

// StatsContext enables metric collection backed by OpenCensus.
type StatsContext struct {
	clientsTotal      *stats.Int64Measure
	requestSizesBytes *stats.Int64Measure
	requestAcksTotal  *stats.Int64Measure
	requestNacksTotal *stats.Int64Measure
	sendFailuresTotal *stats.Int64Measure
	recvFailuresTotal *stats.Int64Measure
}

// SetClientsTotal updates the current client count to the given argument.
func (s *StatsContext) SetClientsTotal(clients int64) {
	stats.Record(context.Background(), s.clientsTotal.M(clients))
}

func (s *StatsContext) recordError(err error, code codes.Code, stat *stats.Int64Measure) {
	ctx, ctxErr := tag.New(context.Background(),
		tag.Insert(ErrorTag, err.Error()),
		tag.Insert(ErrorCodeTag, strconv.FormatUint(uint64(code), 10)))
	if ctxErr != nil {
		scope.Errorf("MCP: error creating monitoring context. %v", ctxErr)
		return
	}
	stats.Record(ctx, stat.M(1))
}

// RecordSendError records an error during a network send with its error
// string and code.
func (s *StatsContext) RecordSendError(err error, code codes.Code) {
	s.recordError(err, code, s.sendFailuresTotal)
}

// RecordRecvError records an error during a network recv with its error
// string and code.
func (s *StatsContext) RecordRecvError(err error, code codes.Code) {
	s.recordError(err, code, s.recvFailuresTotal)
}

// RecordRequestSize records the size of a request from a connection for a specific type URL.
func (s *StatsContext) RecordRequestSize(typeURL string, connectionID int64, size int) {
	ctx, ctxErr := tag.New(context.Background(),
		tag.Insert(TypeURLTag, typeURL),
		tag.Insert(ConnectionIDTag, strconv.FormatInt(connectionID, 10)))
	if ctxErr != nil {
		scope.Errorf("MCP: error creating monitoring context. %v", ctxErr)
		return
	}
	stats.Record(ctx, s.requestSizesBytes.M(int64(size)))
}

// RecordRequestAck records an ACK message for a type URL on a connection.
func (s *StatsContext) RecordRequestAck(typeURL string, connectionID int64) {
	ctx, ctxErr := tag.New(context.Background(),
		tag.Insert(TypeURLTag, typeURL),
		tag.Insert(ConnectionIDTag, strconv.FormatInt(connectionID, 10)))
	if ctxErr != nil {
		scope.Errorf("MCP: error creating monitoring context. %v", ctxErr)
		return
	}
	stats.Record(ctx, s.requestAcksTotal.M(1))
}

// RecordRequestNack records a NACK message for a type URL on a connection.
func (s *StatsContext) RecordRequestNack(typeURL string, connectionID int64) {
	ctx, ctxErr := tag.New(context.Background(),
		tag.Insert(TypeURLTag, typeURL),
		tag.Insert(ConnectionIDTag, strconv.FormatInt(connectionID, 10)))
	if ctxErr != nil {
		scope.Errorf("MCP: error creating monitoring context. %v", ctxErr)
		return
	}
	stats.Record(ctx, s.requestNacksTotal.M(1))
}

// NewStatsContext creates a new context for recording metrics using
// OpenCensus. The specified prefix is prepended to all metric names and must
// be a non-empty string.
func NewStatsContext(prefix string) *StatsContext {
	if len(prefix) == 0 {
		panic("must specify prefix for MCP server monitoring.")
	} else if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	ctx := &StatsContext{
		// ClientsTotal is a measure of the number of connected clients.
		clientsTotal: stats.Int64(
			prefix+"mcp/server/clients_total",
			"The number of clients currently connected.",
			stats.UnitDimensionless),

		// RequestSizesBytes is a distribution of incoming message sizes.
		requestSizesBytes: stats.Int64(
			prefix+"mcp/server/message_sizes_bytes",
			"Size of messages received from clients.",
			stats.UnitBytes),

		// RequestAcksTotal is a measure of the number of received ACK requests.
		requestAcksTotal: stats.Int64(
			prefix+"mcp/server/request_acks_total",
			"The number of request acks received by the server.",
			stats.UnitDimensionless),

		// RequestNacksTotal is a measure of the number of received NACK requests.
		requestNacksTotal: stats.Int64(
			prefix+"mcp/server/request_nacks_total",
			"The number of request nacks received by the server.",
			stats.UnitDimensionless),

		// SendFailuresTotal is a measure of the number of network send failures.
		sendFailuresTotal: stats.Int64(
			prefix+"mcp/server/send_failures_total",
			"The number of send failures in the server.",
			stats.UnitDimensionless),

		// RecvFailuresTotal is a measure of the number of network recv failures.
		recvFailuresTotal: stats.Int64(
			prefix+"mcp/server/recv_failures_total",
			"The number of recv failures in the server.",
			stats.UnitDimensionless),
	}

	err := view.Register(
		newView(ctx.clientsTotal, []tag.Key{}, view.LastValue()),
		newView(ctx.requestSizesBytes, []tag.Key{ConnectionIDTag}, view.Distribution(byteBuckets...)),
		newView(ctx.requestAcksTotal, []tag.Key{TypeURLTag}, view.Count()),
		newView(ctx.requestNacksTotal, []tag.Key{ErrorCodeTag, TypeURLTag}, view.Count()),
		newView(ctx.sendFailuresTotal, []tag.Key{ErrorCodeTag, ErrorTag}, view.Count()),
		newView(ctx.recvFailuresTotal, []tag.Key{ErrorCodeTag, ErrorTag}, view.Count()),
	)

	if err != nil {
		panic(err)
	}

	return ctx
}

var (
	// TypeURLTag holds the type URL for the context.
	TypeURLTag tag.Key
	// ErrorCodeTag holds the gRPC error code for the context.
	ErrorCodeTag tag.Key
	// ErrorTag holds the error string for the context.
	ErrorTag tag.Key
	// ConnectionIDTag holds the connection ID for the context.
	ConnectionIDTag tag.Key

	// buckets are powers of 4
	byteBuckets = []float64{1, 4, 16, 64, 256, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864, 268435456, 1073741824}
)

func newView(measure stats.Measure, keys []tag.Key, aggregation *view.Aggregation) *view.View {
	return &view.View{
		Name:        measure.Name(),
		Description: measure.Description(),
		Measure:     measure,
		TagKeys:     keys,
		Aggregation: aggregation,
	}
}

func init() {
	var err error
	if TypeURLTag, err = tag.NewKey(typeURL); err != nil {
		panic(err)
	}
	if ErrorCodeTag, err = tag.NewKey(errorCode); err != nil {
		panic(err)
	}
	if ErrorTag, err = tag.NewKey(errorStr); err != nil {
		panic(err)
	}
	if ConnectionIDTag, err = tag.NewKey(connectionID); err != nil {
		panic(err)
	}
}
