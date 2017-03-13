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
	"fmt"
	"io"
	"net"
	"testing"

	rpc "github.com/googleapis/googleapis/google/rpc"
	"google.golang.org/grpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/status"
	"istio.io/mixer/pkg/tracing"
)

const (
	testRequestID0 = 1234
	testRequestID1 = 5678
)

type testState struct {
	client     mixerpb.MixerClient
	connection *grpc.ClientConn
	gs         *grpc.Server
}

func (ts *testState) createGRPCServer(port uint16) error {
	// get the network stuff setup
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}

	var grpcOptions []grpc.ServerOption
	grpcOptions = append(grpcOptions, grpc.MaxConcurrentStreams(32))
	grpcOptions = append(grpcOptions, grpc.MaxMsgSize(1024*1024))

	// get everything wired up
	ts.gs = grpc.NewServer(grpcOptions...)
	s := NewGRPCServer(ts, tracing.DisabledTracer())
	mixerpb.RegisterMixerServer(ts.gs, s)

	go func() {
		_ = ts.gs.Serve(listener)
	}()

	return nil
}

func (ts *testState) deleteGRPCServer() {
	ts.gs.GracefulStop()
}

func (ts *testState) createAPIClient(port uint16) error {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())

	var err error
	if ts.connection, err = grpc.Dial(fmt.Sprintf("localhost:%v", port), opts...); err != nil {
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

func prepTestState(port uint16) (*testState, error) {
	ts := &testState{}
	if err := ts.createGRPCServer(port); err != nil {
		return nil, err
	}

	if err := ts.createAPIClient(port); err != nil {
		ts.deleteGRPCServer()
		return nil, err
	}

	return ts, nil
}

func (ts *testState) cleanupTestState() {
	ts.deleteAPIClient()
	ts.deleteGRPCServer()
}

func (ts *testState) Check(ctx context.Context, tracker attribute.Tracker, request *mixerpb.CheckRequest, response *mixerpb.CheckResponse) {
	response.RequestIndex = request.RequestIndex
	response.Result = status.New(rpc.UNIMPLEMENTED)
}

func (ts *testState) Report(ctx context.Context, tracker attribute.Tracker, request *mixerpb.ReportRequest, response *mixerpb.ReportResponse) {
	response.RequestIndex = request.RequestIndex
	response.Result = status.New(rpc.UNIMPLEMENTED)
}

func (ts *testState) Quota(ctx context.Context, tracker attribute.Tracker, request *mixerpb.QuotaRequest, response *mixerpb.QuotaResponse) {
	response.RequestIndex = request.RequestIndex
	response.Result = status.New(rpc.UNIMPLEMENTED)
	response.Amount = 0
}

func TestCheck(t *testing.T) {
	ts, err := prepTestState(29999)
	if err != nil {
		t.Errorf("unable to prep test state %v", err)
		return
	}
	defer ts.cleanupTestState()

	stream, err := ts.client.Check(context.Background())
	if err != nil {
		t.Errorf("Check failed %v", err)
		return
	}

	waitc := make(chan int64)
	go func() {
		for {
			response, err := stream.Recv()
			if err == io.EOF {
				close(waitc)
				return
			} else if err != nil {
				t.Errorf("Failed to receive a response : %v", err)
				return
			} else {
				waitc <- response.RequestIndex
			}
		}
	}()

	// send the first request
	request := mixerpb.CheckRequest{RequestIndex: testRequestID0}
	if err := stream.Send(&request); err != nil {
		t.Errorf("Failed to send first request: %v", err)
	}

	// send the second request
	request = mixerpb.CheckRequest{RequestIndex: testRequestID1}
	if err := stream.Send(&request); err != nil {
		t.Errorf("Failed to send second request: %v", err)
	}

	r0 := <-waitc
	r1 := <-waitc

	if err := stream.CloseSend(); err != nil {
		t.Errorf("Failed to close gRPC stream: %v", err)
	}

	if (r0 == testRequestID0 && r1 == testRequestID1) || (r0 == testRequestID1 && r1 == testRequestID0) {
		t.Log("Worked")
	} else {
		t.Error("Did not receive the two expected responses")
	}

	// wait for the goroutine to be done
	<-waitc
}

func TestReport(t *testing.T) {
	ts, err := prepTestState(30000)
	if err != nil {
		t.Errorf("unable to prep test state %v", err)
		return
	}
	defer ts.cleanupTestState()

	stream, err := ts.client.Report(context.Background())
	if err != nil {
		t.Errorf("Report failed %v", err)
		return
	}

	waitc := make(chan int64)
	go func() {
		for {
			response, err := stream.Recv()
			if err == io.EOF {
				close(waitc)
				return
			} else if err != nil {
				t.Errorf("Failed to receive a response : %v", err)
				return
			} else {
				waitc <- response.RequestIndex
			}
		}
	}()

	// send the first request
	request := mixerpb.ReportRequest{RequestIndex: testRequestID0}
	if err := stream.Send(&request); err != nil {
		t.Errorf("Failed to send first request: %v", err)
	}

	// send the second request
	request = mixerpb.ReportRequest{RequestIndex: testRequestID1}
	if err := stream.Send(&request); err != nil {
		t.Errorf("Failed to send second request: %v", err)
	}

	r0 := <-waitc
	r1 := <-waitc

	if err := stream.CloseSend(); err != nil {
		t.Errorf("Failed to close gRPC stream: %v", err)
	}

	if (r0 == testRequestID0 && r1 == testRequestID1) || (r0 == testRequestID1 && r1 == testRequestID0) {
		t.Log("Worked")
	} else {
		t.Error("Did not receive the two expected responses")
	}

	// wait for the goroutine to be done
	<-waitc
}

func TestQuota(t *testing.T) {
	ts, err := prepTestState(30001)
	if err != nil {
		t.Errorf("unable to prep test state %v", err)
		return
	}
	defer ts.cleanupTestState()

	stream, err := ts.client.Quota(context.Background())
	if err != nil {
		t.Errorf("Quota failed %v", err)
		return
	}

	waitc := make(chan int64)
	go func() {
		for {
			response, err := stream.Recv()
			if err == io.EOF {
				close(waitc)
				return
			} else if err != nil {
				t.Errorf("Failed to receive a response : %v", err)
				return
			} else {
				waitc <- response.RequestIndex
			}
		}
	}()

	// send the first request
	request := mixerpb.QuotaRequest{RequestIndex: testRequestID0}
	if err := stream.Send(&request); err != nil {
		t.Errorf("Failed to send first request: %v", err)
	}

	// send the second request
	request = mixerpb.QuotaRequest{RequestIndex: testRequestID1}
	if err := stream.Send(&request); err != nil {
		t.Errorf("Failed to send second request: %v", err)
	}

	r0 := <-waitc
	r1 := <-waitc

	if err := stream.CloseSend(); err != nil {
		t.Errorf("Failed to close gRPC stream: %v", err)
	}

	if (r0 == testRequestID0 && r1 == testRequestID1) || (r0 == testRequestID1 && r1 == testRequestID0) {
		t.Log("Worked")
	} else {
		t.Error("Did not receive the two expected responses")
	}

	// wait for the goroutine to be done
	<-waitc
}
