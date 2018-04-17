// Copyright 2017, OpenCensus Authors
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
//

package ocgrpc

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

// The following variables are measures are recorded by ClientHandler:
var (
	ClientErrorCount, _       = stats.Int64("grpc.io/client/error_count", "RPC Errors", stats.UnitNone)
	ClientRequestBytes, _     = stats.Int64("grpc.io/client/request_bytes", "Request bytes", stats.UnitBytes)
	ClientResponseBytes, _    = stats.Int64("grpc.io/client/response_bytes", "Response bytes", stats.UnitBytes)
	ClientStartedCount, _     = stats.Int64("grpc.io/client/started_count", "Number of client RPCs (streams) started", stats.UnitNone)
	ClientFinishedCount, _    = stats.Int64("grpc.io/client/finished_count", "Number of client RPCs (streams) finished", stats.UnitNone)
	ClientRequestCount, _     = stats.Int64("grpc.io/client/request_count", "Number of client RPC request messages", stats.UnitNone)
	ClientResponseCount, _    = stats.Int64("grpc.io/client/response_count", "Number of client RPC response messages", stats.UnitNone)
	ClientRoundTripLatency, _ = stats.Float64("grpc.io/client/roundtrip_latency", "RPC roundtrip latency in msecs", stats.UnitMilliseconds)
)

// Predefined views may be subscribed to collect data for the above measures.
// As always, you may also define your own custom views over measures collected by this
// package. These are declared as a convenience only; none are subscribed by
// default.
var (
	ClientErrorCountView = &view.View{
		Name:        "grpc.io/client/error_count",
		Description: "RPC Errors",
		TagKeys:     []tag.Key{KeyStatus, KeyMethod},
		Measure:     ClientErrorCount,
		Aggregation: view.Mean(),
	}

	ClientRoundTripLatencyView = &view.View{
		Name:        "grpc.io/client/roundtrip_latency",
		Description: "Latency in msecs",
		TagKeys:     []tag.Key{KeyMethod},
		Measure:     ClientRoundTripLatency,
		Aggregation: DefaultMillisecondsDistribution,
	}

	ClientRequestBytesView = &view.View{
		Name:        "grpc.io/client/request_bytes",
		Description: "Request bytes",
		TagKeys:     []tag.Key{KeyMethod},
		Measure:     ClientRequestBytes,
		Aggregation: DefaultBytesDistribution,
	}

	ClientResponseBytesView = &view.View{
		Name:        "grpc.io/client/response_bytes",
		Description: "Response bytes",
		TagKeys:     []tag.Key{KeyMethod},
		Measure:     ClientResponseBytes,
		Aggregation: DefaultBytesDistribution,
	}

	ClientRequestCountView = &view.View{
		Name:        "grpc.io/client/request_count",
		Description: "Count of request messages per client RPC",
		TagKeys:     []tag.Key{KeyMethod},
		Measure:     ClientRequestCount,
		Aggregation: DefaultMessageCountDistribution,
	}

	ClientResponseCountView = &view.View{
		Name:        "grpc.io/client/response_count",
		Description: "Count of response messages per client RPC",
		TagKeys:     []tag.Key{KeyMethod},
		Measure:     ClientResponseCount,
		Aggregation: DefaultMessageCountDistribution,
	}
)

// DefaultClientViews are the default client views provided by this package.
var DefaultClientViews = []*view.View{
	ClientErrorCountView,
	ClientRoundTripLatencyView,
	ClientRequestBytesView,
	ClientResponseBytesView,
	ClientRequestCountView,
	ClientResponseCountView,
}

// TODO(jbd): Add roundtrip_latency, uncompressed_request_bytes, uncompressed_response_bytes, request_count, response_count.
// TODO(acetechnologist): This is temporary and will need to be replaced by a
// mechanism to load these defaults from a common repository/config shared by
// all supported languages. Likely a serialized protobuf of these defaults.
