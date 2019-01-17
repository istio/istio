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

package monitoring

import (
	"context"
	"io"
	"strconv"
	"strings"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"google.golang.org/grpc/codes"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/mcp/testing/monitoring"
)

const (
	collection   = "collection"
	errorCode    = "code"
	errorStr     = "error"
	connectionID = "connectionID"
	code         = "code"
)

var (
	scope = log.RegisterScope("mcp", "mcp debugging", 0)
)

// StatsContext enables metric collection backed by OpenCensus.
type StatsContext struct {
	currentStreamCount       *stats.Int64Measure
	requestSizesBytes        *stats.Int64Measure
	requestAcksTotal         *stats.Int64Measure
	requestNacksTotal        *stats.Int64Measure
	sendFailuresTotal        *stats.Int64Measure
	recvFailuresTotal        *stats.Int64Measure
	streamCreateSuccessTotal *stats.Int64Measure

	views []*view.View
}

// Reporter is used to report metrics for an MCP server.
type Reporter interface {
	io.Closer

	RecordSendError(err error, code codes.Code)
	RecordRecvError(err error, code codes.Code)
	RecordRequestSize(collection string, connectionID int64, size int)
	RecordRequestAck(collection string, connectionID int64)
	RecordRequestNack(collection string, connectionID int64, code codes.Code)

	SetStreamCount(clients int64)
	RecordStreamCreateSuccess()
}

var (
	_ Reporter = &StatsContext{}

	// verify here to avoid import cycles in the pkg/mcp/testing/monitoring package
	_ Reporter = &monitoring.InMemoryStatsContext{}
)

// SetStreamCount updates the current client count to the given argument.
func (s *StatsContext) SetStreamCount(clients int64) {
	stats.Record(context.Background(), s.currentStreamCount.M(clients))
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
func (s *StatsContext) RecordRequestSize(collection string, connectionID int64, size int) {
	ctx, ctxErr := tag.New(context.Background(),
		tag.Insert(CollectionTag, collection),
		tag.Insert(ConnectionIDTag, strconv.FormatInt(connectionID, 10)))
	if ctxErr != nil {
		scope.Errorf("MCP: error creating monitoring context. %v", ctxErr)
		return
	}
	stats.Record(ctx, s.requestSizesBytes.M(int64(size)))
}

// RecordRequestAck records an ACK message for a collection on a connection.
func (s *StatsContext) RecordRequestAck(collection string, connectionID int64) {
	ctx, ctxErr := tag.New(context.Background(),
		tag.Insert(CollectionTag, collection),
		tag.Insert(ConnectionIDTag, strconv.FormatInt(connectionID, 10)))
	if ctxErr != nil {
		scope.Errorf("MCP: error creating monitoring context. %v", ctxErr)
		return
	}
	stats.Record(ctx, s.requestAcksTotal.M(1))
}

// RecordRequestNack records a NACK message for a collection on a connection.
func (s *StatsContext) RecordRequestNack(collection string, connectionID int64, code codes.Code) {
	ctx, ctxErr := tag.New(context.Background(),
		tag.Insert(CollectionTag, collection),
		tag.Insert(ConnectionIDTag, strconv.FormatInt(connectionID, 10)),
		tag.Insert(CodeTag, code.String()))
	if ctxErr != nil {
		scope.Errorf("MCP: error creating monitoring context. %v", ctxErr)
		return
	}
	stats.Record(ctx, s.requestNacksTotal.M(1))
}

// RecordStreamCreateSuccess records a successful stream connection.
func (s *StatsContext) RecordStreamCreateSuccess() {
	stats.Record(context.Background(), s.streamCreateSuccessTotal.M(1))
}

func (s *StatsContext) Close() error {
	view.Unregister(s.views...)
	return nil
}

// NewStatsContext creates a new context for recording metrics using
// OpenCensus. The specified prefix is prepended to all metric names and must
// be a non-empty string.
func NewStatsContext(prefix string) *StatsContext {
	if len(prefix) == 0 {
		panic("must specify prefix for MCP monitoring.")
	} else if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	ctx := &StatsContext{
		// StreamTotal is a measure of the number of connected clients.
		currentStreamCount: stats.Int64(
			prefix+"clients_total",
			"The number of streams currently connected.",
			stats.UnitDimensionless),

		// RequestSizesBytes is a distribution of incoming message sizes.
		requestSizesBytes: stats.Int64(
			prefix+"message_sizes_bytes",
			"Size of messages received from clients.",
			stats.UnitBytes),

		// RequestAcksTotal is a measure of the number of received ACK requests.
		requestAcksTotal: stats.Int64(
			prefix+"request_acks_total",
			"The number of request acks received by the source.",
			stats.UnitDimensionless),

		// RequestNacksTotal is a measure of the number of received NACK requests.
		requestNacksTotal: stats.Int64(
			prefix+"request_nacks_total",
			"The number of request nacks received by the source.",
			stats.UnitDimensionless),

		// SendFailuresTotal is a measure of the number of network send failures.
		sendFailuresTotal: stats.Int64(
			prefix+"send_failures_total",
			"The number of send failures in the source.",
			stats.UnitDimensionless),

		// RecvFailuresTotal is a measure of the number of network recv failures.
		recvFailuresTotal: stats.Int64(
			prefix+"recv_failures_total",
			"The number of recv failures in the source.",
			stats.UnitDimensionless),

		streamCreateSuccessTotal: stats.Int64(
			prefix+"reconnections",
			"The number of times the sink has reconnected.",
			stats.UnitDimensionless),
	}

	ctx.addView(ctx.currentStreamCount, []tag.Key{}, view.LastValue())
	ctx.addView(ctx.requestSizesBytes, []tag.Key{ConnectionIDTag}, view.Distribution(byteBuckets...))
	ctx.addView(ctx.requestAcksTotal, []tag.Key{CollectionTag, ConnectionIDTag}, view.Count())
	ctx.addView(ctx.requestNacksTotal, []tag.Key{CollectionTag, ConnectionIDTag, CodeTag}, view.Count())
	ctx.addView(ctx.sendFailuresTotal, []tag.Key{ErrorCodeTag, ErrorTag}, view.Count())
	ctx.addView(ctx.recvFailuresTotal, []tag.Key{ErrorCodeTag, ErrorTag}, view.Count())
	ctx.addView(ctx.streamCreateSuccessTotal, []tag.Key{}, view.Count())

	return ctx
}

func (s *StatsContext) addView(measure stats.Measure, keys []tag.Key, aggregation *view.Aggregation) {
	v := &view.View{
		Name:        measure.Name(),
		Description: measure.Description(),
		Measure:     measure,
		TagKeys:     keys,
		Aggregation: aggregation,
	}

	if err := view.Register(v); err != nil {
		panic(err)
	}

	s.views = append(s.views, v)
}

var (
	// CollectionTag holds the collection for the context.
	CollectionTag tag.Key
	// ErrorCodeTag holds the gRPC error code for the context.
	ErrorCodeTag tag.Key
	// ErrorTag holds the error string for the context.
	ErrorTag tag.Key
	// ConnectionIDTag holds the connection ID for the context.
	ConnectionIDTag tag.Key
	// CodeTag holds the status code for the context.
	CodeTag tag.Key

	// buckets are powers of 4
	byteBuckets = []float64{1, 4, 16, 64, 256, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864, 268435456, 1073741824}
)

func init() {
	var err error
	if CollectionTag, err = tag.NewKey(collection); err != nil {
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
	if CodeTag, err = tag.NewKey(code); err != nil {
		panic(err)
	}
}
