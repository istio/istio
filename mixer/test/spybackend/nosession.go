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

//go:generate go run $GOPATH/src/istio.io/istio/mixer/tools/mixgen/main.go adapter -n spybackend-nosession -s=false -t metric -t quota -t listentry -o nosession.yaml

package spybackend

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"

	"google.golang.org/grpc"

	adptModel "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/template/listentry"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/quota"
)

type (
	// Server is basic server interface
	Server interface {
		Addr() net.Addr
		Close() error
		Run()
	}

	// NoSessionServer models no session adapter backend.
	NoSessionServer struct {
		listener net.Listener
		shutdown chan error
		server   *grpc.Server
		Behavior *Behavior
		Requests *Requests
	}
)

var _ metric.HandleMetricServiceServer = &NoSessionServer{}
var _ listentry.HandleListEntryServiceServer = &NoSessionServer{}
var _ quota.HandleQuotaServiceServer = &NoSessionServer{}

// HandleMetric records metric entries and responds with the programmed response
func (s *NoSessionServer) HandleMetric(c context.Context, r *metric.HandleMetricRequest) (*adptModel.ReportResult, error) {
	s.Requests.metricLock.Lock()
	s.Requests.HandleMetricRequest = append(s.Requests.HandleMetricRequest, r)
	s.Requests.metricLock.Unlock()
	return s.Behavior.HandleMetricResult, s.Behavior.HandleMetricError
}

// HandleListEntry records listrequest and responds with the programmed response
func (s *NoSessionServer) HandleListEntry(c context.Context, r *listentry.HandleListEntryRequest) (*adptModel.CheckResult, error) {
	s.Requests.listentryLock.Lock()
	s.Requests.HandleListEntryRequest = append(s.Requests.HandleListEntryRequest, r)
	s.Requests.listentryLock.Unlock()
	return s.Behavior.HandleListEntryResult, s.Behavior.HandleListEntryError
}

// HandleQuota records quotarequest and responds with the programmed response
func (s *NoSessionServer) HandleQuota(c context.Context, r *quota.HandleQuotaRequest) (*adptModel.QuotaResult, error) {
	s.Requests.quotaLock.Lock()
	s.Requests.HandleQuotaRequest = append(s.Requests.HandleQuotaRequest, r)
	s.Requests.quotaLock.Unlock()
	return s.Behavior.HandleQuotaResult, s.Behavior.HandleQuotaError
}

// Addr returns the listening address of the server
func (s *NoSessionServer) Addr() net.Addr {
	return s.listener.Addr()
}

// Run starts the server run
func (s *NoSessionServer) Run() {
	s.shutdown = make(chan error, 1)
	go func() {
		err := s.server.Serve(s.listener)

		// notify closer we're done
		s.shutdown <- err
	}()
}

// Wait waits for server to stop
func (s *NoSessionServer) Wait() error {
	if s.shutdown == nil {
		return fmt.Errorf("server not running")
	}

	err := <-s.shutdown
	s.shutdown = nil
	return err
}

// Close gracefully shuts down the server
func (s *NoSessionServer) Close() error {
	if s.shutdown != nil {
		s.server.GracefulStop()
		_ = s.Wait()
	}

	if s.listener != nil {
		_ = s.listener.Close()
	}

	return nil
}

// GetState returns the adapters observed state.
func (s *NoSessionServer) GetState() interface{} {
	result := make([]interface{}, 0)
	result = append(result, s.printMetrics()...)
	result = append(result, s.printListEntry()...)
	result = append(result, s.printQuota()...)
	result = append(result, s.printValidationRequest()...)
	return result
}

const stripText = "stripped_for_test"

func (s *NoSessionServer) printMetrics() []interface{} {
	result := make([]interface{}, 0)

	s.Requests.metricLock.RLock()
	defer s.Requests.metricLock.RUnlock()

	if len(s.Requests.HandleMetricRequest) > 0 {
		// Stable sort order for varieties.
		for _, mr := range s.Requests.HandleMetricRequest {
			mr.DedupId = stripText
		}
		sort.SliceStable(s.Requests.HandleMetricRequest, func(i, j int) bool {
			return strings.Compare(fmt.Sprintf("%v", s.Requests.HandleMetricRequest[i].Instances),
				fmt.Sprintf("%v", s.Requests.HandleMetricRequest[j].Instances)) > 0
		})

		for _, mr := range s.Requests.HandleMetricRequest {
			result = append(result, *mr)
		}
	}
	return result
}

func (s *NoSessionServer) printListEntry() []interface{} {
	result := make([]interface{}, 0)

	s.Requests.listentryLock.RLock()
	defer s.Requests.listentryLock.RUnlock()

	if len(s.Requests.HandleListEntryRequest) > 0 {
		// Stable sort order for varieties.
		for _, mr := range s.Requests.HandleListEntryRequest {
			mr.DedupId = stripText
		}
		sort.Slice(s.Requests.HandleListEntryRequest, func(i, j int) bool {
			return strings.Compare(fmt.Sprintf("%v", s.Requests.HandleListEntryRequest[i].Instance),
				fmt.Sprintf("%v", s.Requests.HandleListEntryRequest[j].Instance)) > 0
		})

		for _, mr := range s.Requests.HandleListEntryRequest {
			result = append(result, *mr)
		}
	}
	return result
}

func (s *NoSessionServer) printQuota() []interface{} {
	result := make([]interface{}, 0)

	s.Requests.quotaLock.RLock()
	defer s.Requests.quotaLock.RUnlock()

	if len(s.Requests.HandleQuotaRequest) > 0 {
		// Stable sort order for varieties.
		for _, mr := range s.Requests.HandleQuotaRequest {
			mr.DedupId = stripText
		}
		sort.Slice(s.Requests.HandleQuotaRequest, func(i, j int) bool {
			return strings.Compare(fmt.Sprintf("%v", s.Requests.HandleQuotaRequest[i].Instance),
				fmt.Sprintf("%v", s.Requests.HandleQuotaRequest[j].Instance)) > 0
		})

		for _, mr := range s.Requests.HandleQuotaRequest {
			result = append(result, *mr)
		}
	}
	return result
}

func (s *NoSessionServer) printValidationRequest() []interface{} {
	result := make([]interface{}, 0)

	if len(s.Requests.ValidateRequest) > 0 {
		// Stable sort order for varieties.
		sort.Slice(s.Requests.ValidateRequest, func(i, j int) bool {
			return strings.Compare(fmt.Sprintf("%v", s.Requests.ValidateRequest[i].InferredTypes),
				fmt.Sprintf("%v", s.Requests.ValidateRequest[j].InferredTypes)) > 0
		})

		for _, mr := range s.Requests.ValidateRequest {
			result = append(result, *mr)
		}
	}
	return result
}

// NewNoSessionServer creates a new no session server from given args.
func NewNoSessionServer(a *Args) (Server, error) {
	s := &NoSessionServer{Behavior: a.Behavior, Requests: a.Requests}
	var err error

	if s.listener, err = net.Listen("tcp", fmt.Sprintf(":%d", 0)); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("unable to listen on socket: %v", err)
	}

	fmt.Printf("listening on :%v", s.listener.Addr())

	s.server = grpc.NewServer()
	metric.RegisterHandleMetricServiceServer(s.server, s)
	listentry.RegisterHandleListEntryServiceServer(s.server, s)
	quota.RegisterHandleQuotaServiceServer(s.server, s)

	return s, nil
}
