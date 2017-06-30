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
	"istio.io/mixer/pkg/adapterManager"
	"istio.io/mixer/pkg/api"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/pool"
)

type Handler struct {
	stress   bool
	ch       chan *attribute.MutableBag
	count    int
	r_status rpc.Status
}

func newHandler(stress bool) *Handler {
	h := &Handler{
		stress:   stress,
		count:    0,
		r_status: rpc.Status{},
	}
	if !stress {
		h.ch = make(chan *attribute.MutableBag, 100) // Allow maximum 100 requests
	}
	return h
}

func (h *Handler) run(bag *attribute.MutableBag) rpc.Status {
	if !h.stress {
		h.ch <- attribute.CopyBag(bag)
	}
	h.count++
	return h.r_status
}

type MixerServer struct {
	adapterManager.AspectDispatcher

	lis net.Listener
	gs  *grpc.Server
	gp  *pool.GoroutinePool
	s   mixerpb.MixerServer

	check  *Handler
	report *Handler
	quota  *Handler

	qma          *aspect.QuotaMethodArgs
	quota_amount int64
	quota_limit  int64
}

func (ts *MixerServer) Preprocess(ctx context.Context, bag, output *attribute.MutableBag) rpc.Status {
	return rpc.Status{Code: int32(rpc.OK)}
}

func (ts *MixerServer) Check(ctx context.Context, bag *attribute.MutableBag,
	output *attribute.MutableBag) rpc.Status {
	return ts.check.run(bag)
}

func (ts *MixerServer) Report(ctx context.Context, bag *attribute.MutableBag) rpc.Status {
	return ts.report.run(bag)
}

func (ts *MixerServer) Quota(ctx context.Context, bag *attribute.MutableBag,
	qma *aspect.QuotaMethodArgs) (*aspect.QuotaMethodResp, rpc.Status) {
	*ts.qma = *qma
	status := ts.quota.run(bag)
	qmr := &aspect.QuotaMethodResp{}
	if status.Code == 0 {
		if ts.quota_limit == 0 {
			qmr.Amount = qma.Amount
		} else {
			if ts.quota_amount < ts.quota_limit {
				delta := ts.quota_limit - ts.quota_amount
				if delta > qma.Amount {
					qmr.Amount = qma.Amount
				} else {
					qmr.Amount = delta
				}
			}
			ts.quota_amount += qmr.Amount
		}
		qmr.Expiration = time.Minute
	}
	return qmr, status
}

func NewMixerServer(port uint16, stress bool) (*MixerServer, error) {
	log.Printf("Mixer server listening on port %v\n", port)
	s := &MixerServer{
		check:  newHandler(stress),
		report: newHandler(stress),
		quota:  newHandler(stress),
		qma:    &aspect.QuotaMethodArgs{},
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

	s.s = api.NewGRPCServer(s, s.gp)
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
