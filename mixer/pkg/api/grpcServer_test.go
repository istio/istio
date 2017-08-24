// Copyright 2016 Istio Authors
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

package api

import (
	"context"
	"flag"
	"fmt"
	"net"
	"strings"
	"testing"

	rpc "github.com/googleapis/googleapis/google/rpc"
	"google.golang.org/grpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/adapterManager"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/pool"
	"istio.io/mixer/pkg/status"
)

type preprocCallback func(requestBag attribute.Bag, responseBag *attribute.MutableBag) rpc.Status
type checkCallback func(requestBag attribute.Bag, responseBag *attribute.MutableBag) rpc.Status
type reportCallback func(requestBag attribute.Bag) rpc.Status
type quotaCallback func(requestBag attribute.Bag, args *aspect.QuotaMethodArgs) (*aspect.QuotaMethodResp,
	rpc.Status)

type testState struct {
	adapterManager.AspectDispatcher

	client     mixerpb.MixerClient
	connection *grpc.ClientConn
	gs         *grpc.Server
	gp         *pool.GoroutinePool
	s          *grpcServer

	check   checkCallback
	report  reportCallback
	quota   quotaCallback
	preproc preprocCallback
}

func (ts *testState) createGRPCServer() (string, error) {
	// get the network stuff setup
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", 0))
	if err != nil {
		return "", err
	}

	var grpcOptions []grpc.ServerOption
	grpcOptions = append(grpcOptions, grpc.MaxConcurrentStreams(32))
	grpcOptions = append(grpcOptions, grpc.MaxMsgSize(1024*1024))

	// get everything wired up
	ts.gs = grpc.NewServer(grpcOptions...)

	ts.gp = pool.NewGoroutinePool(128, false)
	ts.gp.AddWorkers(32)

	ms := NewGRPCServer(ts, nil, ts.gp)
	ts.s = ms.(*grpcServer)
	mixerpb.RegisterMixerServer(ts.gs, ts.s)

	go func() {
		_ = ts.gs.Serve(listener)
	}()

	return listener.Addr().String(), nil
}

func (ts *testState) deleteGRPCServer() {
	ts.gs.GracefulStop()
	ts.gp.Close()
}

func (ts *testState) createAPIClient(dial string) error {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())

	var err error
	if ts.connection, err = grpc.Dial(dial, opts...); err != nil {
		return err
	}

	ts.client = mixerpb.NewMixerClient(ts.connection)
	return nil
}

func (ts *testState) deleteAPIClient() {
	_ = ts.connection.Close()
	ts.client = nil
	ts.connection = nil
}

func prepTestState() (*testState, error) {
	ts := &testState{}
	dial, err := ts.createGRPCServer()
	if err != nil {
		return nil, err
	}

	if err = ts.createAPIClient(dial); err != nil {
		ts.deleteGRPCServer()
		return nil, err
	}

	ts.preproc = func(requestBag attribute.Bag, responseBag *attribute.MutableBag) rpc.Status {
		return status.OK
	}

	return ts, nil
}

func (ts *testState) cleanupTestState() {
	ts.deleteAPIClient()
	ts.deleteGRPCServer()
}

func (ts *testState) Check(ctx context.Context, bag attribute.Bag, output *attribute.MutableBag) rpc.Status {
	return ts.check(bag, output)
}

func (ts *testState) Report(ctx context.Context, bag attribute.Bag) rpc.Status {
	return ts.report(bag)
}

func (ts *testState) Quota(ctx context.Context, bag attribute.Bag,
	qma *aspect.QuotaMethodArgs) (*aspect.QuotaMethodResp, rpc.Status) {

	return ts.quota(bag, qma)
}

func (ts *testState) Preprocess(ctx context.Context, bag attribute.Bag, output *attribute.MutableBag) rpc.Status {
	return ts.preproc(bag, output)
}

func TestCheck(t *testing.T) {
	ts, err := prepTestState()
	if err != nil {
		t.Fatalf("Unable to prep test state: %v", err)
	}
	defer ts.cleanupTestState()

	ts.check = func(requestBag attribute.Bag, responseBag *attribute.MutableBag) rpc.Status {
		return status.WithPermissionDenied("Not Implemented")
	}

	ts.quota = func(requestBag attribute.Bag, args *aspect.QuotaMethodArgs) (*aspect.QuotaMethodResp, rpc.Status) {
		qmr := &aspect.QuotaMethodResp{Amount: 42}
		return qmr, status.OK
	}

	attr0 := mixerpb.Attributes{
		Words: []string{"A1", "A2", "A3"},
		Int64S: map[int32]int64{
			-1: 25,
			-2: 26,
			-3: 27,
		},
	}

	request := mixerpb.CheckRequest{Attributes: attr0}
	request.Quotas = make(map[string]mixerpb.CheckRequest_QuotaParams)
	request.Quotas["RequestCount"] = mixerpb.CheckRequest_QuotaParams{Amount: 42}

	response, err := ts.client.Check(context.Background(), &request)

	if err != nil {
		t.Errorf("Got %v, expected success", err)
	} else if status.IsOK(response.Precondition.Status) {
		t.Error("Got precondition success, expected error")
	} else if !strings.Contains(response.Precondition.Status.Message, "Not Implemented") {
		t.Errorf("'%s' doesn't contain 'Not Implemented'", response.Precondition.Status.Message)
	} else if response.Quotas["RequestCount"].GrantedAmount != 42 {
		t.Errorf("Got %v granted amount, expecting 42", response.Quotas["RequestCount"].GrantedAmount)
	}

	ts.quota = func(requestBag attribute.Bag, args *aspect.QuotaMethodArgs) (*aspect.QuotaMethodResp, rpc.Status) {
		return nil, status.WithPermissionDenied("Not Implemented")
	}

	_, err = ts.client.Check(context.Background(), &request)

	if err != nil {
		t.Errorf("Got %v, expected success", err)
	}
}

func TestReport(t *testing.T) {
	ts, err := prepTestState()
	if err != nil {
		t.Fatalf("Unable to prep test state: %v", err)
	}
	defer ts.cleanupTestState()

	ts.report = func(requestBag attribute.Bag) rpc.Status {
		return status.OK
	}

	request := mixerpb.ReportRequest{Attributes: []mixerpb.Attributes{{}}}
	_, err = ts.client.Report(context.Background(), &request)
	if err != nil {
		t.Errorf("Expected success, got error: %v", err)
	}

	ts.report = func(requestBag attribute.Bag) rpc.Status {
		return status.WithPermissionDenied("Not Implemented")
	}

	_, err = ts.client.Report(context.Background(), &request)
	if err == nil {
		t.Errorf("Got success, expected failure")
	}

	// test out delta encoding of attributes
	attr0 := mixerpb.Attributes{
		Words: []string{"A1", "A2", "A3"},
		Int64S: map[int32]int64{
			-1: 25,
			-2: 26,
			-3: 27,
		},
	}

	attr1 := mixerpb.Attributes{
		Words: []string{"A1", "A2", "A3"},
		Int64S: map[int32]int64{
			-2: 42,
		},
	}

	callCount := 0
	ts.report = func(requestBag attribute.Bag) rpc.Status {
		v1, _ := requestBag.Get("A1")
		v2, _ := requestBag.Get("A2")
		v3, _ := requestBag.Get("A3")

		i1 := v1.(int64)
		i2 := v2.(int64)
		i3 := v3.(int64)

		if callCount == 0 {
			if i1 != 25 || i2 != 26 || i3 != 27 {
				t.Errorf("Got %d %d %d, expected 25 26 27", i1, i2, i3)
			}
		} else if callCount == 1 {
			if i1 != 25 || i2 != 42 || i3 != 27 {
				t.Errorf("Got %d %d %d, expected 25 42 27", i1, i2, i3)
			}

		} else {
			t.Errorf("Dispatched to Report method more than twice")
		}
		callCount++
		return status.OK
	}

	request = mixerpb.ReportRequest{Attributes: []mixerpb.Attributes{attr0, attr1}}
	_, _ = ts.client.Report(context.Background(), &request)

	if callCount == 0 {
		t.Errorf("Got %d, expected call count of 2", callCount)
	}
}

func TestUnknownStatus(t *testing.T) {
	ts, err := prepTestState()
	if err != nil {
		t.Fatalf("Unable to prep test state: %v", err)
	}
	defer ts.cleanupTestState()

	ts.check = func(requestBag attribute.Bag, responseBag *attribute.MutableBag) rpc.Status {
		return rpc.Status{
			Code:    12345678,
			Message: "DEADBEEF!",
		}
	}

	request := mixerpb.CheckRequest{}
	resp, err := ts.client.Check(context.Background(), &request)
	if err != nil {
		t.Error("Got failure, expected success")
	} else if !strings.Contains(resp.Precondition.Status.Message, "DEADBEEF!") {
		t.Errorf("Got '%s', expected DEADBEEF!", resp.Precondition.Status.Message)
	}
}

func TestFailingPreproc(t *testing.T) {
	ts, err := prepTestState()
	if err != nil {
		t.Fatalf("Unable to prep test state: %v", err)
	}
	defer ts.cleanupTestState()

	ts.preproc = func(requestBag attribute.Bag, responseBag *attribute.MutableBag) rpc.Status {
		return rpc.Status{
			Code:    12345678,
			Message: "DEADBEEF!",
		}
	}

	{
		request := mixerpb.CheckRequest{}
		resp, err := ts.client.Check(context.Background(), &request)
		if resp != nil {
			t.Error("Expecting no response, got one")
		}
		if err == nil {
			t.Error("Got success, expected failure")
		} else if !strings.Contains(err.Error(), "DEADBEEF!") {
			t.Errorf("Got '%s', expected DEADBEEF!", err.Error())
		}
	}

	{
		request := mixerpb.ReportRequest{Attributes: []mixerpb.Attributes{{}}}
		resp, err := ts.client.Report(context.Background(), &request)
		if resp != nil {
			t.Error("Expecting no response, got one")
		}
		if err == nil {
			t.Error("Got success, expected failure")
		} else if !strings.Contains(err.Error(), "DEADBEEF!") {
			t.Errorf("Got '%s', expected DEADBEEF!", err.Error())
		}
	}
}

func init() {
	// bump up the log level so log-only logic runs during the tests, for correctness and coverage.
	_ = flag.Lookup("v").Value.Set("99")
}
