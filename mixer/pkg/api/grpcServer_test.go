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

type testState struct {
	adapterManager.AspectDispatcher

	client         mixerpb.MixerClient
	connection     *grpc.ClientConn
	gs             *grpc.Server
	gp             *pool.GoroutinePool
	s              *grpcServer
	reportBadAttr  bool
	failPreprocess bool
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

	ms := NewGRPCServer(ts, ts.gp)
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

	return ts, nil
}

func (ts *testState) cleanupTestState() {
	ts.deleteAPIClient()
	ts.deleteGRPCServer()
}

func (ts *testState) Check(ctx context.Context, bag *attribute.MutableBag, output *attribute.MutableBag) rpc.Status {
	return status.WithPermissionDenied("Not Implemented")
}

func (ts *testState) Report(ctx context.Context, bag *attribute.MutableBag, output *attribute.MutableBag) rpc.Status {
	if ts.reportBadAttr {
		// we inject an attribute with an unsupported type to trigger an error path
		output.Set("BADATTR", 0)
	}

	return status.WithPermissionDenied("Not Implemented")
}

func (ts *testState) Quota(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag,
	qma *aspect.QuotaMethodArgs) (*aspect.QuotaMethodResp, rpc.Status) {

	qmr := &aspect.QuotaMethodResp{Amount: 42}
	return qmr, status.OK
}

func (ts *testState) Preprocess(ctx context.Context, bag, output *attribute.MutableBag) rpc.Status {
	output.Set("preprocess_attribute", "true")
	if ts.failPreprocess {
		return status.WithInternal("failed process")
	}
	return status.OK
}

func TestCheck(t *testing.T) {
	ts, err := prepTestState()
	if err != nil {
		t.Fatalf("Unable to prep test state: %v", err)
	}
	defer ts.cleanupTestState()

	request := mixerpb.CheckRequest{}
	response, err := ts.client.Check(context.Background(), &request)

	if err != nil {
		t.Errorf("Got %v, expected success", err)
	} else if status.IsOK(response.Status) {
		t.Error("Got success, expected error")
	} else if !strings.Contains(response.Status.Message, "Not Implemented") {
		t.Errorf("'%s' doesn't contain 'Not Implemented'", response.Status.Message)
	}
}

func TestReport(t *testing.T) {
	ts, err := prepTestState()
	if err != nil {
		t.Fatalf("Unable to prep test state: %v", err)
	}
	defer ts.cleanupTestState()

	request := mixerpb.ReportRequest{}
	_, err = ts.client.Report(context.Background(), &request)
	if err != nil {
		t.Errorf("Expected success, got error: %v", err)
	}
}

func TestQuota(t *testing.T) {
	ts, err := prepTestState()
	if err != nil {
		t.Fatalf("Unable to prep test state: %v", err)
	}
	defer ts.cleanupTestState()

	request := mixerpb.QuotaRequest{}
	_, err = ts.client.Quota(context.Background(), &request)

	if err != nil {
		t.Errorf("Expected success, got error: %v", err)
	}
}

/*
func TestOverload(t *testing.T) {
	ts, err := prepTestState()
	if err != nil {
		t.Fatalf("Unable to prep test state: %v", err)
	}
	defer ts.cleanupTestState()

	stream, err := ts.client.Report(context.Background())
	if err != nil {
		t.Fatalf("Report failed %v", err)
	}

	const numMessages = 16384
	waitc := make(chan int64, numMessages)
	go func() {
		for {
			response, err := stream.Recv()
			if err == io.EOF {
				close(waitc)
				return
			} else if err != nil {
				t.Errorf("Failed to receive a response: %v", err)
				return
			} else {
				waitc <- response.RequestIndex
			}
		}
	}()

	for i := 0; i < numMessages; i++ {
		request := mixerpb.ReportRequest{RequestIndex: int64(i)}
		if err := stream.Send(&request); err != nil {
			t.Errorf("Failed to send request %d: %v", i, err)
		}
	}

	results := make(map[int64]bool, numMessages)
	for i := 0; i < numMessages; i++ {
		results[<-waitc] = true
	}

	for i := 0; i < numMessages; i++ {
		if !results[int64(i)] {
			t.Errorf("Didn't get response for message %d", i)
		}
	}

	if err := stream.CloseSend(); err != nil {
		t.Errorf("Failed to close gRPC stream: %v", err)
	}

	// wait for the goroutine to be done
	<-waitc
}

func TestBadBag(t *testing.T) {
	ts, err := prepTestState()
	if err != nil {
		t.Fatalf("Unable to prep test state: %v", err)
	}
	defer ts.cleanupTestState()

	wg := sync.WaitGroup{}
	wg.Add(1)

	// install a failing SendMsg to exercise the failure path
	ts.s.sendMsg = func(stream grpc.Stream, m proto.Message) error {
		err = errors.New("nothing good")
		wg.Done()
		return err
	}

	ts.reportBadAttr = true

	stream, err := ts.client.Report(context.Background())
	if err != nil {
		t.Fatalf("Report failed %v", err)
	}

	request := mixerpb.ReportRequest{}
	if err := stream.Send(&request); err != nil {
		t.Errorf("Failed to send request: %v", err)
	}

	// we never hear back from the server (since the send failed). Just wait
	// for the goroutine to signal it's done
	wg.Wait()
}
*/

func init() {
	// bump up the log level so log-only logic runs during the tests, for correctness and coverage.
	_ = flag.Lookup("v").Value.Set("99")
}
