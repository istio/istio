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

package env

import (
	"fmt"
	"log"
	"net"
	"time"

	"google.golang.org/grpc"

	mixerpb "istio.io/api/mixer/v1"
	rpc "istio.io/gogo-genproto/googleapis/google/rpc"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/mockapi"
)

// Handler stores data for Check, Quota and Report.
type Handler struct {
	stress  bool
	ch      chan *attribute.MutableBag
	count   int
	rStatus rpc.Status
}

func newHandler(stress bool) *Handler {
	h := &Handler{
		stress:  stress,
		count:   0,
		rStatus: rpc.Status{},
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
	return h.rStatus
}

// MixerServer stores data for a mock Mixer server.
type MixerServer struct {
	mockapi.AttributesHandler

	lis net.Listener
	gs  *grpc.Server

	check  *Handler
	report *Handler
	quota  *Handler

	qma         mockapi.QuotaArgs
	quotaAmount int64
	quotaLimit  int64

	checkReferenced *mixerpb.ReferencedAttributes
	quotaReferenced *mixerpb.ReferencedAttributes
}

// Check is called by the mock mixer api
func (ts *MixerServer) Check(bag attribute.Bag, output *attribute.MutableBag) (mockapi.CheckResponse, rpc.Status) {
	result := mockapi.CheckResponse{
		ValidDuration: mockapi.DefaultValidDuration,
		ValidUseCount: mockapi.DefaultValidUseCount,
		Referenced:    ts.checkReferenced,
	}
	return result, ts.check.run(bag)
}

// Report is called by the mock mixer api
func (ts *MixerServer) Report(bag attribute.Bag) rpc.Status {
	return ts.report.run(bag)
}

// Quota is called by the mock mixer api
func (ts *MixerServer) Quota(bag attribute.Bag, qma mockapi.QuotaArgs) (mockapi.QuotaResponse, rpc.Status) {
	ts.qma = qma
	status := ts.quota.run(bag)
	qmr := mockapi.QuotaResponse{}
	if status.Code == 0 {
		if ts.quotaLimit == 0 {
			qmr.Amount = qma.Amount
		} else {
			if ts.quotaAmount < ts.quotaLimit {
				delta := ts.quotaLimit - ts.quotaAmount
				if delta > qma.Amount {
					qmr.Amount = qma.Amount
				} else {
					qmr.Amount = delta
				}
			}
			ts.quotaAmount += qmr.Amount
		}
		qmr.Expiration = time.Minute
	}
	qmr.Referenced = ts.quotaReferenced
	return qmr, status
}

// NewMixerServer creates a new Mixer server
func NewMixerServer(port uint16, stress bool) (*MixerServer, error) {
	log.Printf("Mixer server listening on port %v\n", port)
	s := &MixerServer{
		check:  newHandler(stress),
		report: newHandler(stress),
		quota:  newHandler(stress),
		qma:    mockapi.QuotaArgs{},
	}

	var err error
	s.lis, err = net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
		return nil, err
	}

	attrSrv := mockapi.NewAttributesServer(s)
	s.gs = mockapi.NewMixerServer(attrSrv)
	return s, nil
}

// Start starts the mixer server
func (ts *MixerServer) Start() {
	go func() {
		err := ts.gs.Serve(ts.lis)
		if err != nil {
			log.Fatalf("failed to start mixer server: %v", err)
		}
		log.Printf("Mixer server starts\n")
	}()
}

// Stop shutdown the server
func (ts *MixerServer) Stop() {
	log.Printf("Stop Mixer server\n")
	ts.gs.Stop()
	log.Printf("Stop Mixer server  -- Done\n")
}
