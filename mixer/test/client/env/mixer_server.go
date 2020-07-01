// Copyright Istio Authors
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
	"sync"
	"time"

	"google.golang.org/grpc"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/mockapi"
	"istio.io/pkg/attribute"
)

// Handler stores data for Check, Quota and Report.
type Handler struct {
	stress  bool
	ch      chan *attribute.MutableBag
	count   int
	mutex   *sync.Mutex
	rStatus rpc.Status
}

func newHandler(stress bool) *Handler {
	h := &Handler{
		stress:  stress,
		count:   0,
		mutex:   &sync.Mutex{},
		rStatus: rpc.Status{},
	}
	if !stress {
		h.ch = make(chan *attribute.MutableBag, 100) // Allow maximum 100 requests
	}
	return h
}

func (h *Handler) run(bag attribute.Bag) rpc.Status {
	if !h.stress {
		h.mutex.Lock()
		h.count++
		h.mutex.Unlock()
		h.ch <- attribute.CopyBag(bag)
	}
	return h.rStatus
}

// Count get the called counter
func (h *Handler) Count() int {
	h.mutex.Lock()
	c := h.count
	h.mutex.Unlock()
	return c
}

// MixerServer stores data for a mock Mixer server.
type MixerServer struct {
	mockapi.AttributesHandler

	lis net.Listener
	gs  *grpc.Server

	check  *Handler
	report *Handler
	quota  *Handler

	quotaAmount int64
	quotaLimit  int64

	checkReferenced *mixerpb.ReferencedAttributes
	quotaReferenced *mixerpb.ReferencedAttributes

	directive *mixerpb.RouteDirective

	sync.Mutex
	qma mockapi.QuotaArgs
}

// Check is called by the mock mixer api
func (ts *MixerServer) Check(bag attribute.Bag) mixerpb.CheckResponse_PreconditionResult {
	return mixerpb.CheckResponse_PreconditionResult{
		Status:               ts.check.run(bag),
		ValidDuration:        mockapi.DefaultValidDuration,
		ValidUseCount:        mockapi.DefaultValidUseCount,
		ReferencedAttributes: ts.checkReferenced,
		RouteDirective:       ts.directive,
	}
}

// Report is called by the mock mixer api
func (ts *MixerServer) Report(bag attribute.Bag) rpc.Status {
	return ts.report.run(bag)
}

// Quota is called by the mock mixer api
func (ts *MixerServer) Quota(bag attribute.Bag, qma mockapi.QuotaArgs) (mockapi.QuotaResponse, rpc.Status) {
	ts.Lock()
	defer ts.Unlock()

	if !ts.quota.stress {
		// In non-stress case, saved for test verification
		ts.qma = qma
	}
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
func NewMixerServer(port uint16, stress bool, checkDict bool, checkSourceUID string) (*MixerServer, error) {
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

	attrSrv := mockapi.NewAttributesServer(s, checkDict)
	if checkSourceUID != "" {
		attrSrv.SetCheckMetadata(func(attrs *mixerpb.Attributes) error {
			value := attrs.Attributes["source.uid"].GetStringValue()
			if value != checkSourceUID {
				return fmt.Errorf("got source.uid = %q, expected %q", value, checkSourceUID)
			}
			return nil
		})
	}
	s.gs = mockapi.NewMixerServer(attrSrv)
	return s, nil
}

// Start starts the mixer server
func (ts *MixerServer) Start() <-chan error {
	errCh := make(chan error)

	go func() {
		err := ts.gs.Serve(ts.lis)
		if err != nil {
			errCh <- fmt.Errorf("failed to start mixer server: %v", err)
		}
	}()

	// wait for grpc server up
	time.AfterFunc(1*time.Second, func() { close(errCh) })
	return errCh
}

// Stop shutdown the server
func (ts *MixerServer) Stop() {
	log.Printf("Stop Mixer server\n")
	ts.gs.Stop()
	log.Printf("Stop Mixer server  -- Done\n")
}

// GetReport will return a received report
func (ts *MixerServer) GetReport() *attribute.MutableBag {
	return <-ts.report.ch
}

// GetCheck will return a received check
func (ts *MixerServer) GetCheck() *attribute.MutableBag {
	return <-ts.check.ch
}
