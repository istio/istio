// Copyright 2016 Google Inc.
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

package main

import (
	"context"
	"fmt"
	"io"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/grpc"

	"istio.io/mixer/server/attribute"

	mixerpb "istio.io/mixer/api/v1"
)

const (
	testPort       = 29999
	testRequestID0 = 1234
	testRequestID1 = 5678
)

type testState struct {
	apiServer  *APIServer
	client     mixerpb.MixerClient
	connection *grpc.ClientConn
}

func (ts *testState) createAPIServer() error {
	options := APIServerOptions{
		Port:                 testPort,
		MaxMessageSize:       1024 * 1024,
		MaxConcurrentStreams: 32,
		CompressedPayload:    false,
		ServerCertificate:    nil,
		ClientCertificates:   nil,
		Handlers:             ts,
		AttributeManager:     attribute.NewManager(),
	}

	var err error
	if ts.apiServer, err = NewAPIServer(&options); err != nil {
		return err
	}

	go ts.apiServer.Start()
	return nil
}

func (ts *testState) deleteAPIServer() {
	ts.apiServer.Stop()
	ts.apiServer = nil
}

func (ts *testState) createAPIClient() error {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())

	var err error
	if ts.connection, err = grpc.Dial(fmt.Sprintf("localhost:%v", testPort), opts...); err != nil {
		return err
	}

	ts.client = mixerpb.NewMixerClient(ts.connection)
	return nil
}

func (ts *testState) deleteAPIClient() {
	ts.connection.Close()
	ts.client = nil
	ts.connection = nil
}

func prepTestState() (*testState, error) {
	ts := &testState{}
	if err := ts.createAPIServer(); err != nil {
		return nil, err
	}

	if err := ts.createAPIClient(); err != nil {
		ts.deleteAPIServer()
		return nil, err
	}

	return ts, nil
}

func (ts *testState) cleanupTestState() {
	ts.deleteAPIClient()
	ts.deleteAPIServer()
}

func (ts *testState) Check(tracker attribute.Tracker, request *mixerpb.CheckRequest, response *mixerpb.CheckResponse) {
	response.RequestIndex = request.RequestIndex
	response.Result = newStatus(code.Code_UNIMPLEMENTED)
}

func (ts *testState) Report(tracker attribute.Tracker, request *mixerpb.ReportRequest, response *mixerpb.ReportResponse) {
	response.RequestIndex = request.RequestIndex
	response.Result = newStatus(code.Code_UNIMPLEMENTED)
}

func (ts *testState) Quota(tracker attribute.Tracker, request *mixerpb.QuotaRequest, response *mixerpb.QuotaResponse) {
	response.RequestIndex = request.RequestIndex
	response.Result = newQuotaError(code.Code_UNIMPLEMENTED)
}

func TestCheck(t *testing.T) {
	ts, err := prepTestState()
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

	stream.CloseSend()

	if (r0 == testRequestID0 && r1 == testRequestID1) || (r0 == testRequestID1 && r1 == testRequestID0) {
		t.Log("Worked")
	} else {
		t.Error("Did not receive the two expected responses")
	}

	// wait for the goroutine to be done
	_ = <-waitc
}

func TestReport(t *testing.T) {
	ts, err := prepTestState()
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

	stream.CloseSend()

	if (r0 == testRequestID0 && r1 == testRequestID1) || (r0 == testRequestID1 && r1 == testRequestID0) {
		t.Log("Worked")
	} else {
		t.Error("Did not receive the two expected responses")
	}

	// wait for the goroutine to be done
	_ = <-waitc
}
