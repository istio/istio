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

// nolint:lll
//go:generate go run $REPO_ROOT/mixer/tools/mixgen/main.go adapter -n spybackend-nosession -s=false -t metric -t quota -t listentry -t apa -t checkoutput -o nosession.yaml -d example

package spybackend

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	adptModel "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/template/listentry"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/quota"
	spyadapter "istio.io/istio/mixer/test/spyAdapter"
	sampleapa "istio.io/istio/mixer/test/spyAdapter/template/apa"
	samplecheck "istio.io/istio/mixer/test/spyAdapter/template/check"
	checkoutputTmpl "istio.io/istio/mixer/test/spyAdapter/template/checkoutput"
	samplequota "istio.io/istio/mixer/test/spyAdapter/template/quota"
	samplereport "istio.io/istio/mixer/test/spyAdapter/template/report"
)

type (
	// Server is basic server interface
	Server interface {
		Addr() net.Addr
		Close() error
		Run()
		GetCapturedCalls() []spyadapter.CapturedCall
	}

	// NoSessionServer models no session adapter backend.
	NoSessionServer struct {
		listener      net.Listener
		shutdown      chan error
		server        *grpc.Server
		Behavior      *Behavior
		Requests      *Requests
		CapturedCalls []spyadapter.CapturedCall
	}
)

var _ metric.HandleMetricServiceServer = &NoSessionServer{}
var _ listentry.HandleListEntryServiceServer = &NoSessionServer{}
var _ quota.HandleQuotaServiceServer = &NoSessionServer{}
var _ samplecheck.HandleSampleCheckServiceServer = &NoSessionServer{}
var _ samplequota.HandleSampleQuotaServiceServer = &NoSessionServer{}
var _ samplereport.HandleSampleReportServiceServer = &NoSessionServer{}
var _ sampleapa.HandleSampleApaServiceServer = &NoSessionServer{}
var _ checkoutputTmpl.HandleCheckProducerServiceServer = &NoSessionServer{}

// HandleMetric records metric entries and responds with the programmed response
func (s *NoSessionServer) HandleMetric(c context.Context, r *metric.HandleMetricRequest) (*adptModel.ReportResult, error) {
	if !verifyHeader(c, s.Behavior.HeaderKey, s.Behavior.HeaderToken) {
		return nil, fmt.Errorf("want auth header %v %v", s.Behavior.HeaderKey, s.Behavior.HeaderToken)
	}
	s.Requests.metricLock.Lock()
	s.Requests.HandleMetricRequest = append(s.Requests.HandleMetricRequest, r)
	s.Requests.metricLock.Unlock()
	time.Sleep(s.Behavior.HandleMetricSleep)
	return s.Behavior.HandleMetricResult, s.Behavior.HandleMetricError
}

// HandleListEntry records listrequest and responds with the programmed response
func (s *NoSessionServer) HandleListEntry(c context.Context, r *listentry.HandleListEntryRequest) (*adptModel.CheckResult, error) {
	s.Requests.listentryLock.Lock()
	s.Requests.HandleListEntryRequest = append(s.Requests.HandleListEntryRequest, r)
	s.Requests.listentryLock.Unlock()
	time.Sleep(s.Behavior.HandleListEntrySleep)
	return s.Behavior.HandleListEntryResult, s.Behavior.HandleListEntryError
}

// HandleQuota records quotarequest and responds with the programmed response
func (s *NoSessionServer) HandleQuota(c context.Context, r *quota.HandleQuotaRequest) (*adptModel.QuotaResult, error) {
	s.Requests.quotaLock.Lock()
	s.Requests.HandleQuotaRequest = append(s.Requests.HandleQuotaRequest, r)
	s.Requests.quotaLock.Unlock()
	time.Sleep(s.Behavior.HandleQuotaSleep)
	return s.Behavior.HandleQuotaResult, s.Behavior.HandleQuotaError
}

// HandleSampleCheck records samplecheck and responds with the programmed response
func (s *NoSessionServer) HandleSampleCheck(c context.Context, r *samplecheck.HandleSampleCheckRequest) (*adptModel.CheckResult, error) {
	cc := spyadapter.CapturedCall{
		Name:      "HandleSampleCheck",
		Instances: []interface{}{r.Instance},
	}
	if s.CapturedCalls == nil {
		s.CapturedCalls = []spyadapter.CapturedCall{}
	}
	s.CapturedCalls = append(s.CapturedCalls, cc)
	time.Sleep(s.Behavior.HandleSampleCheckSleep)
	return s.Behavior.HandleSampleCheckResult, s.Behavior.HandleSampleCheckError
}

// HandleSampleReport records samplereport and responds with the programmed response
func (s *NoSessionServer) HandleSampleReport(c context.Context, r *samplereport.HandleSampleReportRequest) (*adptModel.ReportResult, error) {
	cc := spyadapter.CapturedCall{
		Name:      "HandleSampleReport",
		Instances: make([]interface{}, len(r.Instances)),
	}
	for i, ins := range r.Instances {
		cc.Instances[i] = ins
	}

	if s.CapturedCalls == nil {
		s.CapturedCalls = []spyadapter.CapturedCall{}
	}
	s.CapturedCalls = append(s.CapturedCalls, cc)
	time.Sleep(s.Behavior.HandleSampleReportSleep)
	return s.Behavior.HandleSampleReportResult, s.Behavior.HandleSampleReportError
}

// HandleSampleQuota records samplequota and responds with the programmed response
func (s *NoSessionServer) HandleSampleQuota(c context.Context, r *samplequota.HandleSampleQuotaRequest) (*adptModel.QuotaResult, error) {
	cc := spyadapter.CapturedCall{
		Name:      "HandleSampleQuota",
		Instances: []interface{}{r.Instance},
	}
	if s.CapturedCalls == nil {
		s.CapturedCalls = []spyadapter.CapturedCall{}
	}
	s.CapturedCalls = append(s.CapturedCalls, cc)
	time.Sleep(s.Behavior.HandleSampleQuotaSleep)
	return s.Behavior.HandleSampleQuotaResult, s.Behavior.HandleSampleQuotaError
}

// HandleSampleApa records sampleapa and responds with the programmed response
func (s *NoSessionServer) HandleSampleApa(c context.Context, r *sampleapa.HandleSampleApaRequest) (*sampleapa.OutputMsg, error) {
	s.CapturedCalls = append(s.CapturedCalls, spyadapter.CapturedCall{
		Name:      "HandleSampleApa",
		Instances: []interface{}{r.Instance},
	})
	time.Sleep(s.Behavior.HandleSampleApaSleep)
	return s.Behavior.HandleSampleApaResult, s.Behavior.HandleSampleApaError
}

// HandleCheckProducer records checkoutput and responds with the programmed response
func (s *NoSessionServer) HandleCheckProducer(c context.Context,
	r *checkoutputTmpl.HandleCheckProducerRequest) (*checkoutputTmpl.HandleCheckProducerResponse, error) {
	s.CapturedCalls = append(s.CapturedCalls, spyadapter.CapturedCall{
		Name:      "HandleCheckProducer",
		Instances: []interface{}{r.Instance},
	})
	return &checkoutputTmpl.HandleCheckProducerResponse{
		Result: s.Behavior.HandleSampleCheckResult,
		Output: s.Behavior.HandleCheckOutput,
	}, s.Behavior.HandleSampleCheckError
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

func (s *NoSessionServer) GetCapturedCalls() []spyadapter.CapturedCall {
	return s.CapturedCalls
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

	if s.listener, err = net.Listen("tcp", fmt.Sprintf(":%d", 50051)); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("unable to listen on socket: %v", err)
	}
	fmt.Printf("listening on :%v", s.listener.Addr())

	if a.Behavior.RequireMTls || a.Behavior.RequireTLS {
		certificate, err := tls.LoadX509KeyPair(
			a.Behavior.CredsPath,
			a.Behavior.KeyPath,
		)
		if err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("cannot load key cert pair %v", err)
		}
		certPool := x509.NewCertPool()
		bs, err := ioutil.ReadFile(a.Behavior.CertPath)
		if err != nil {
			log.Fatalf("failed to read client ca cert: %s", err)
		}

		ok := certPool.AppendCertsFromPEM(bs)
		if !ok {
			log.Fatal("failed to append client certs")
		}

		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{certificate},
			ClientCAs:    certPool,
		}
		if a.Behavior.RequireMTls {
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
			tlsConfig.InsecureSkipVerify = a.Behavior.InsecureSkipVerification
		}
		serverOption := grpc.Creds(credentials.NewTLS(tlsConfig))
		s.server = grpc.NewServer(serverOption)
	} else {
		s.server = grpc.NewServer()
	}

	metric.RegisterHandleMetricServiceServer(s.server, s)
	listentry.RegisterHandleListEntryServiceServer(s.server, s)
	quota.RegisterHandleQuotaServiceServer(s.server, s)
	samplereport.RegisterHandleSampleReportServiceServer(s.server, s)
	samplecheck.RegisterHandleSampleCheckServiceServer(s.server, s)
	samplequota.RegisterHandleSampleQuotaServiceServer(s.server, s)
	sampleapa.RegisterHandleSampleApaServiceServer(s.server, s)
	checkoutputTmpl.RegisterHandleCheckProducerServiceServer(s.server, s)

	return s, nil
}

func verifyHeader(c context.Context, key, value string) bool {
	if key == "" {
		return true
	}
	md, ok := metadata.FromIncomingContext(c)
	if !ok {
		return false
	}
	if val, ok := md[key]; ok && len(val) == 1 && val[0] == value {
		return true
	}
	return false
}
