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
	"sync/atomic"
	"time"

	ocstats "go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"golang.org/x/net/context"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
)

// statsTagRPC gets the tag.Map populated by the application code, serializes
// its tags into the GRPC metadata in order to be sent to the server.
func (h *ClientHandler) statsTagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	startTime := time.Now()
	if info == nil {
		if grpclog.V(2) {
			grpclog.Infof("clientHandler.TagRPC called with nil info.", info.FullMethodName)
		}
		return ctx
	}

	d := &rpcData{
		startTime: startTime,
		method:    info.FullMethodName,
	}
	ts := tag.FromContext(ctx)
	if ts != nil {
		encoded := tag.Encode(ts)
		ctx = stats.SetTags(ctx, encoded)
	}

	// TODO(acetechnologist): should we be recording this later? What is the
	// point of updating d.reqLen & d.reqCount if we update now?
	record(ctx, d, "", ClientStartedCount.M(1))
	return context.WithValue(ctx, grpcClientRPCKey, d)
}

// statsHandleRPC processes the RPC events.
func (h *ClientHandler) statsHandleRPC(ctx context.Context, s stats.RPCStats) {
	switch st := s.(type) {
	case *stats.Begin, *stats.OutHeader, *stats.InHeader, *stats.InTrailer, *stats.OutTrailer:
		// do nothing for client
	case *stats.OutPayload:
		h.handleRPCOutPayload(ctx, st)
	case *stats.InPayload:
		h.handleRPCInPayload(ctx, st)
	case *stats.End:
		h.handleRPCEnd(ctx, st)
	default:
		grpclog.Infof("unexpected stats: %T", st)
	}
}

func (h *ClientHandler) handleRPCOutPayload(ctx context.Context, s *stats.OutPayload) {
	d, ok := ctx.Value(grpcClientRPCKey).(*rpcData)
	if !ok {
		if grpclog.V(2) {
			grpclog.Infoln("clientHandler.handleRPCOutPayload failed to retrieve *rpcData from context")
		}
		return
	}

	record(ctx, d, "", ClientRequestBytes.M(int64(s.Length)))
	atomic.AddInt64(&d.reqCount, 1)
}

func (h *ClientHandler) handleRPCInPayload(ctx context.Context, s *stats.InPayload) {
	d, ok := ctx.Value(grpcClientRPCKey).(*rpcData)
	if !ok {
		if grpclog.V(2) {
			grpclog.Infoln("failed to retrieve *rpcData from context")
		}
		return
	}

	record(ctx, d, "", ClientResponseBytes.M(int64(s.Length)))
	atomic.AddInt64(&d.respCount, 1)
}

func (h *ClientHandler) handleRPCEnd(ctx context.Context, s *stats.End) {
	d, ok := ctx.Value(grpcClientRPCKey).(*rpcData)
	if !ok {
		if grpclog.V(2) {
			grpclog.Infoln("failed to retrieve *rpcData from context")
		}
		return
	}

	elapsedTime := time.Since(d.startTime)
	reqCount := atomic.LoadInt64(&d.reqCount)
	respCount := atomic.LoadInt64(&d.respCount)

	m := []ocstats.Measurement{
		ClientRequestCount.M(reqCount),
		ClientResponseCount.M(respCount),
		ClientFinishedCount.M(1),
		ClientRoundTripLatency.M(float64(elapsedTime) / float64(time.Millisecond)),
	}

	var st string
	if s.Error != nil {
		s, ok := status.FromError(s.Error)
		if ok {
			st = s.Code().String()
		}
		m = append(m, ClientErrorCount.M(1))
	}
	record(ctx, d, st, m...)
}
