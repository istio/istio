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

var (
	// TypeURLTag holds the type URL for the context.
	TypeURLTag tag.Key
	// ErrorCodeTag holds the gRPC error code for the context.
	ErrorCodeTag tag.Key
	// ErrorTag holds the error string for the context.
	ErrorTag tag.Key
	// ConnectionIdTag holds the connection ID for the context.
	ConnectionIDTag tag.Key

	// buckets are powers of 4
	byteBuckets = []float64{1, 4, 16, 64, 256, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864, 268435456, 1073741824}

	// ClientsTotal is a measure of the number of connected clients.
	ClientsTotal = stats.Int64(
		"mcp/server/clients_total",
		"The number of clients currently connected.",
		stats.UnitDimensionless)

	// RequestSizesBytes is a distribution of incoming message sizes.
	RequestSizesBytes = stats.Int64(
		"mcp/server/message_sizes_bytes",
		"Size of messages received from clients.",
		stats.UnitBytes)

	// RequestAcksTotal is a measure of the number of received ACK requests.
	RequestAcksTotal = stats.Int64(
		"mcp/server/request_acks_total",
		"The number of request acks received by the server.",
		stats.UnitDimensionless)

	// RequestNacksTotal is a measure of the number of received NACK requests.
	RequestNacksTotal = stats.Int64(
		"mcp/server/request_nacks_total",
		"The number of request nacks received by the server.",
		stats.UnitDimensionless)

	// SendFailuresTotal is a measure of the number of network send failures.
	SendFailuresTotal = stats.Int64(
		"mcp/server/send_failures_total",
		"The number of send failures in the server.",
		stats.UnitDimensionless)

	// RecvFailuresTotal is a measure of the number of network recv failures.
	RecvFailuresTotal = stats.Int64(
		"mcp/server/recv_failures_total",
		"The number of recv failures in the server.",
		stats.UnitDimensionless)
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

func recordError(err error, code codes.Code, stat *stats.Int64Measure) {
	ctx, err := tag.New(context.Background(),
		tag.Insert(ErrorTag, err.Error()),
		tag.Insert(ErrorCodeTag, strconv.FormatUint(uint64(code), 10)))
	stats.Record(ctx, stat.M(1))
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

	err = view.Register(
		newView(ClientsTotal, []tag.Key{}, view.LastValue()),
		newView(RequestSizesBytes, []tag.Key{ConnectionIDTag}, view.Distribution(byteBuckets...)),
		newView(RequestAcksTotal, []tag.Key{TypeURLTag}, view.Count()),
		newView(RequestNacksTotal, []tag.Key{ErrorCodeTag, TypeURLTag}, view.Count()),
		newView(SendFailuresTotal, []tag.Key{ErrorCodeTag, ErrorTag}, view.Count()),
		newView(RecvFailuresTotal, []tag.Key{ErrorCodeTag, ErrorTag}, view.Count()),
	)

	if err != nil {
		panic(err)
	}
}
