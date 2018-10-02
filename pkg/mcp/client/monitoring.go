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

package client

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"strings"
	"google.golang.org/grpc/codes"
	"strconv"
	"context"
)

const (
	typeURL   = "typeURL"
	errorCode = "code"
	errorStr  = "error"
)

type StatsContext struct {
	requestAcksTotal         *stats.Int64Measure
	requestNacksTotal        *stats.Int64Measure
	sendFailuresTotal        *stats.Int64Measure
	recvFailuresTotal        *stats.Int64Measure
	streamCreateSuccessTotal *stats.Int64Measure
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

// RecordRequestAck records an ACK message for a type URL on a connection.
func (s *StatsContext) RecordRequestAck(typeURL string) {
	ctx, ctxErr := tag.New(context.Background(), tag.Insert(TypeURLTag, typeURL))
	if ctxErr != nil {
		scope.Errorf("MCP: error creating monitoring context. %v", ctxErr)
		return
	}
	stats.Record(ctx, s.requestAcksTotal.M(1))
}

// RecordRequestNack records a NACK message for a type URL on a connection.
func (s *StatsContext) RecordRequestNack(typeURL string, err error) {
	ctx, ctxErr := tag.New(context.Background(),
		tag.Insert(TypeURLTag, typeURL),
		tag.Insert(ErrorTag, err.Error()))
	if ctxErr != nil {
		scope.Errorf("MCP: error creating monitoring context. %v", ctxErr)
		return
	}
	stats.Record(ctx, s.requestNacksTotal.M(1))
}

func (s *StatsContext) RecordStreamCreateSuccess() {
	stats.Record(context.Background(), s.streamCreateSuccessTotal.M(1))
}

func NewStatsContext(prefix string) *StatsContext {
	if len(prefix) == 0 {
		panic("must specify prefix for MCP client monitoring.")
	} else if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	ctx := &StatsContext{
		requestAcksTotal: stats.Int64(
			prefix+"mcp/client/request_acks",
			"The number of request acks sent by the client.",
			stats.UnitDimensionless),

		requestNacksTotal: stats.Int64(
			prefix+"mcp/client/request_nacks",
			"The number of request nacks sent by the client.",
			stats.UnitDimensionless),

		sendFailuresTotal: stats.Int64(
			prefix+"mcp/client/send_failures",
			"The number of send failures in the client.",
			stats.UnitDimensionless),

		recvFailuresTotal: stats.Int64(
			prefix+"mcp/client/recv_failures",
			"The number of recv failures in the client.",
			stats.UnitDimensionless),

		streamCreateSuccessTotal: stats.Int64(
			prefix+"mcp/client/reconnections",
			"The number of times the client has reconnected.",
			stats.UnitDimensionless),
	}

	err := view.Register(
		newView(ctx.requestAcksTotal, []tag.Key{TypeURLTag}, view.Count()),
		newView(ctx.requestNacksTotal, []tag.Key{ErrorTag, TypeURLTag}, view.Count()),
		newView(ctx.sendFailuresTotal, []tag.Key{ErrorCodeTag, ErrorTag}, view.Count()),
		newView(ctx.recvFailuresTotal, []tag.Key{ErrorCodeTag, ErrorTag}, view.Count()),
		newView(ctx.streamCreateSuccessTotal, []tag.Key{}, view.Count()),
	)

	if err != nil {
		panic(err)
	}
	return ctx
}

var (
	TypeURLTag   tag.Key
	ErrorCodeTag tag.Key
	ErrorTag     tag.Key
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
}
