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
	"fmt"
	"log"
	"net"
	"time"

	rpc "github.com/googleapis/googleapis/google/rpc"
	"google.golang.org/grpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/test"
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

func (h *Handler) run(bag attribute.Bag) rpc.Status {
	if !h.stress {
		h.ch <- attribute.CopyBag(bag)
	}
	h.count++
	return h.r_status
}

type MixerServer struct {
	test.AttributesHandler

	lis net.Listener
	gs  *grpc.Server

	check  *Handler
	report *Handler
	quota  *Handler

	qma          test.QuotaArgs
	quota_amount int64
	quota_limit  int64

	check_referenced *mixerpb.ReferencedAttributes
	quota_referenced *mixerpb.ReferencedAttributes
}

func (ts *MixerServer) Check(bag attribute.Bag, output *attribute.MutableBag) (test.CheckResponse, rpc.Status) {
	result := test.CheckResponse{
		ValidDuration: test.DefaultValidDuration,
		ValidUseCount: test.DefaultValidUseCount,
		Referenced:    ts.check_referenced,
	}
	return result, ts.check.run(bag)
}

func (ts *MixerServer) Report(bag attribute.Bag) rpc.Status {
	return ts.report.run(bag)
}

func (ts *MixerServer) Quota(bag attribute.Bag, qma test.QuotaArgs) (test.QuotaResponse, rpc.Status) {
	ts.qma = qma
	status := ts.quota.run(bag)
	qmr := test.QuotaResponse{}
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
	qmr.Referenced = ts.quota_referenced
	return qmr, status
}

func NewMixerServer(port uint16, stress bool) (*MixerServer, error) {
	log.Printf("Mixer server listening on port %v\n", port)
	s := &MixerServer{
		check:  newHandler(stress),
		report: newHandler(stress),
		quota:  newHandler(stress),
		qma:    test.QuotaArgs{},
	}

	var err error
	s.lis, err = net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
		return nil, err
	}

	attrSrv := test.NewAttributesServer(s)
	s.gs = test.NewMixerServer(attrSrv)
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
