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
	"strings"
	"time"

	ocstats "go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"golang.org/x/net/context"
)

type grpcInstrumentationKey string

// rpcData holds the instrumentation RPC data that is needed between the start
// and end of an call. It holds the info that this package needs to keep track
// of between the various GRPC events.
type rpcData struct {
	// reqCount and respCount has to be the first words
	// in order to be 64-aligned on 32-bit architectures.
	reqCount, respCount int64 // access atomically

	// startTime represents the time at which TagRPC was invoked at the
	// beginning of an RPC. It is an appoximation of the time when the
	// application code invoked GRPC code.
	startTime time.Time
	method    string
}

// The following variables define the default hard-coded auxiliary data used by
// both the default GRPC client and GRPC server metrics.
var (
	DefaultBytesDistribution        = view.Distribution(0, 1024, 2048, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864, 268435456, 1073741824, 4294967296)
	DefaultMillisecondsDistribution = view.Distribution(0, 1, 2, 3, 4, 5, 6, 8, 10, 13, 16, 20, 25, 30, 40, 50, 65, 80, 100, 130, 160, 200, 250, 300, 400, 500, 650, 800, 1000, 2000, 5000, 10000, 20000, 50000, 100000)
	DefaultMessageCountDistribution = view.Distribution(0, 1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384, 32768, 65536)
)

var (
	KeyMethod, _ = tag.NewKey("method")           // gRPC service and method name
	KeyStatus, _ = tag.NewKey("canonical_status") // Canonical status code
)

var (
	grpcServerConnKey = grpcInstrumentationKey("server-conn")
	grpcServerRPCKey  = grpcInstrumentationKey("server-rpc")
	grpcClientRPCKey  = grpcInstrumentationKey("client-rpc")
)

func methodName(fullname string) string {
	return strings.TrimLeft(fullname, "/")
}

func record(ctx context.Context, data *rpcData, status string, m ...ocstats.Measurement) {
	mods := []tag.Mutator{
		tag.Upsert(KeyMethod, methodName(data.method)),
	}
	if status != "" {
		mods = append(mods, tag.Upsert(KeyStatus, status))
	}
	ctx, _ = tag.New(ctx, mods...)
	ocstats.Record(ctx, m...)
}
