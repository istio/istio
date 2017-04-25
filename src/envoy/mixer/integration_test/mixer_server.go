// Copyright 2017 Istio Authors. All Rights Reserved.
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

package test

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	rpc "github.com/googleapis/googleapis/google/rpc"
	"google.golang.org/grpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/api"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/pool"
	"istio.io/mixer/pkg/tracing"
)

type Handler struct {
	bag      *attribute.MutableBag
	ch       chan int
	count    int
	r_status rpc.Status
}

func newHandler() *Handler {
	return &Handler{
		bag:      nil,
		ch:       make(chan int, 10), // Allow maximum 10 requests
		count:    0,
		r_status: rpc.Status{},
	}
}

func (h *Handler) run(bag *attribute.MutableBag) rpc.Status {
	h.bag = attribute.CopyBag(bag)
	h.ch <- 1
	h.count++
	return h.r_status
}

type MixerServer struct {
	lis net.Listener
	gs  *grpc.Server
	gp  *pool.GoroutinePool
	s   mixerpb.MixerServer

	check         *Handler
	report        *Handler
	quota         *Handler
	quota_request *mixerpb.QuotaRequest
}

func (ts *MixerServer) Check(ctx context.Context, bag *attribute.MutableBag,
	request *mixerpb.CheckRequest, response *mixerpb.CheckResponse) {
	response.RequestIndex = request.RequestIndex
	response.Result = ts.check.run(bag)
}

func (ts *MixerServer) Report(ctx context.Context, bag *attribute.MutableBag,
	request *mixerpb.ReportRequest, response *mixerpb.ReportResponse) {
	response.RequestIndex = request.RequestIndex
	response.Result = ts.report.run(bag)
}

func (ts *MixerServer) Quota(ctx context.Context, bag *attribute.MutableBag,
	request *mixerpb.QuotaRequest, response *mixerpb.QuotaResponse) {
	response.RequestIndex = request.RequestIndex
	ts.quota_request = request
	response.Result = ts.quota.run(bag)
	if response.Result.Code != 0 {
		response.Amount = 0
	} else {
		response.Amount = request.Amount
		response.Expiration = time.Minute
	}
}

func NewMixerServer(port uint16) (*MixerServer, error) {
	log.Printf("Mixer server listening on port %v\n", port)
	s := &MixerServer{
		check:  newHandler(),
		report: newHandler(),
		quota:  newHandler(),
	}

	var err error
	s.lis, err = net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
		return nil, err
	}

	var opts []grpc.ServerOption
	opts = append(opts, grpc.MaxConcurrentStreams(32))
	opts = append(opts, grpc.MaxMsgSize(1024*1024))
	s.gs = grpc.NewServer(opts...)

	s.gp = pool.NewGoroutinePool(128, false)
	s.gp.AddWorkers(32)

	s.s = api.NewGRPCServer(s, tracing.DisabledTracer(), s.gp)
	mixerpb.RegisterMixerServer(s.gs, s.s)
	return s, nil
}

func (s *MixerServer) Start() {
	go func() {
		_ = s.gs.Serve(s.lis)
		log.Printf("Mixer server exited\n")
	}()
}

func (s *MixerServer) Stop() {
	log.Printf("Stop Mixer server\n")
	s.gs.Stop()
	log.Printf("Stop Mixer server  -- Done\n")
}
