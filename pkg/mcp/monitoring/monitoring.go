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
	"io"
	"strconv"

	"google.golang.org/grpc/codes"

	testing "istio.io/istio/pkg/mcp/testing/monitoring"
	"istio.io/pkg/monitoring"
)

const (
	collection = "collection"
	errorCode  = "code"
	errorStr   = "error"
	code       = "code"
	component  = "component"
)

var (
	collectionTag = monitoring.MustCreateLabel(collection)
	errorCodeTag  = monitoring.MustCreateLabel(errorCode)
	errorTag      = monitoring.MustCreateLabel(errorStr)
	codeTag       = monitoring.MustCreateLabel(code)
	componentTag  = monitoring.MustCreateLabel(component)

	// currentStreamCount is a measure of the number of connected clients.
	currentStreamCount = monitoring.NewGauge(
		"istio_mcp_clients_total",
		"The number of streams currently connected.",
		monitoring.WithLabels(componentTag),
	)

	// messageSizesBytes is a distribution of message size for each collection.
	messageSizesBytes = monitoring.NewDistribution(
		"istio_mcp_message_sizes_bytes",
		"Size of sent and received messages.",
		// This metric is expected to be used to adjust the maximum
		// gRPC message size for reliable MCP delivery. As such, the
		// distribution bounds are defined in terms of typical max
		// gRPC messages. Though we don't expect people to reduce the
		// default gRPC message, its still useful to see ranges less
		// than the default 4mb to get an early indicator of how the
		// message size is growing.
		[]float64{
			262144 * .8,  // 80% of 256kib
			525288 * .8,  // 80% of 512kib
			1.049e6 * .8, // 80% of 1mib
			2.097e6 * .8, // 80% of 2mib
			4.194e6 * .8, // 80% of 4mib, the default gRPC max message size
			8.389e6 * .8, // 80% of 8mib
			1.258e7 * .8, // 80% of 12mib
			1.678e7 * .8, // 80% of 16mib
		},
		monitoring.WithLabels(componentTag, collectionTag),
		monitoring.WithUnit(monitoring.Bytes),
	)

	// requestAcksTotal is a measure of the number of received ACK requests.
	requestAcksTotal = monitoring.NewSum(
		"istio_mcp_request_acks_total",
		"The number of request acks received by the source.",
		monitoring.WithLabels(componentTag, collectionTag),
	)

	// requestNacksTotal is a measure of the number of received NACK requests.
	requestNacksTotal = monitoring.NewSum(
		"istio_mcp_request_nacks_total",
		"The number of request nacks received by the source.",
		monitoring.WithLabels(componentTag, collectionTag, codeTag),
	)

	// sendFailuresTotal is a measure of the number of network send failures.
	sendFailuresTotal = monitoring.NewSum(
		"istio_mcp_send_failures_total",
		"The number of send failures in the source.",
		monitoring.WithLabels(componentTag, errorCodeTag, errorTag),
	)

	// recvFailuresTotal is a measure of the number of network recv failures.
	recvFailuresTotal = monitoring.NewSum(
		"istio_mcp_recv_failures_total",
		"The number of recv failures in the source.",
		monitoring.WithLabels(componentTag, errorCodeTag, errorTag),
	)

	streamCreateSuccessTotal = monitoring.NewSum(
		"istio_mcp_reconnections",
		"The number of times the sink has reconnected.",
		monitoring.WithLabels(componentTag),
	)
)

// StatsContext enables metric collection backed by OpenCensus.
type StatsContext struct {
	currentStreamCount       monitoring.Metric
	messageSizeBytes         monitoring.Metric
	requestAcksTotal         monitoring.Metric
	requestNacksTotal        monitoring.Metric
	sendFailuresTotal        monitoring.Metric
	recvFailuresTotal        monitoring.Metric
	streamCreateSuccessTotal monitoring.Metric
}

// Reporter is used to report metrics for an MCP server.
type Reporter interface {
	io.Closer

	RecordSendError(err error, code codes.Code)
	RecordRecvError(err error, code codes.Code)
	RecordMessageSize(collection string, connectionID int64, size int)
	RecordRequestAck(collection string, connectionID int64)
	RecordRequestNack(collection string, connectionID int64, code codes.Code)

	SetStreamCount(clients int64)
	RecordStreamCreateSuccess()
}

var (
	_ Reporter = &StatsContext{}

	// verify here to avoid import cycles in the pkg/mcp/testing/monitoring package
	_ Reporter = &testing.InMemoryStatsContext{}
)

// SetStreamCount updates the current client count to the given argument.
func (s *StatsContext) SetStreamCount(clients int64) {
	s.currentStreamCount.Record(float64(clients))
}

func recordError(err error, code codes.Code, m monitoring.Metric) {
	errMetric := m.With(
		errorTag.Value(err.Error()),
		errorCodeTag.Value(strconv.FormatUint(uint64(code), 10)),
	)
	errMetric.Increment()
}

// RecordSendError records an error during a network send with its error
// string and code.
func (s *StatsContext) RecordSendError(err error, code codes.Code) {
	recordError(err, code, s.sendFailuresTotal)
}

// RecordRecvError records an error during a network recv with its error
// string and code.
func (s *StatsContext) RecordRecvError(err error, code codes.Code) {
	recordError(err, code, s.recvFailuresTotal)
}

// RecordMessageSize records the size of a response message for a specific collection
func (s *StatsContext) RecordMessageSize(collection string, connectionID int64, size int) {
	s.messageSizeBytes.With(
		collectionTag.Value(collection),
	).Record(float64(size))
}

// RecordRequestAck records an ACK message for a collection on a connection.
func (s *StatsContext) RecordRequestAck(collection string, connectionID int64) {
	s.requestAcksTotal.With(
		collectionTag.Value(collection),
	).Increment()
}

// RecordRequestNack records a NACK message for a collection on a connection.
func (s *StatsContext) RecordRequestNack(collection string, connectionID int64, code codes.Code) {
	s.requestNacksTotal.With(
		collectionTag.Value(collection),
		codeTag.Value(code.String()),
	).Increment()
}

// RecordStreamCreateSuccess records a successful stream connection.
func (s *StatsContext) RecordStreamCreateSuccess() {
	s.streamCreateSuccessTotal.Increment()
}

func (s *StatsContext) Close() error {
	return nil
}

// NewStatsContext creates a new context for recording MCP-related metrics.
func NewStatsContext(componentName string) *StatsContext {
	if len(componentName) == 0 {
		panic("must specify component for MCP monitoring.")
	}
	ctx := &StatsContext{
		currentStreamCount:       currentStreamCount.With(componentTag.Value(componentName)),
		messageSizeBytes:         messageSizesBytes.With(componentTag.Value(componentName)),
		requestAcksTotal:         requestAcksTotal.With(componentTag.Value(componentName)),
		requestNacksTotal:        requestNacksTotal.With(componentTag.Value(componentName)),
		sendFailuresTotal:        sendFailuresTotal.With(componentTag.Value(componentName)),
		recvFailuresTotal:        recvFailuresTotal.With(componentTag.Value(componentName)),
		streamCreateSuccessTotal: streamCreateSuccessTotal.With(componentTag.Value(componentName)),
	}

	return ctx
}

func init() {
	monitoring.MustRegister(
		currentStreamCount,
		messageSizesBytes,
		requestAcksTotal,
		requestNacksTotal,
		sendFailuresTotal,
		recvFailuresTotal,
		streamCreateSuccessTotal,
	)
}
