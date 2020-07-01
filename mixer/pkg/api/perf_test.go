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

package api

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	_ "google.golang.org/grpc/encoding/gzip" // register the gzip compressor

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/adapter"
	attr "istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/loadshedding"
	"istio.io/istio/mixer/pkg/runtime/dispatcher"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/pkg/attribute"
	"istio.io/pkg/log"
	"istio.io/pkg/pool"
)

type benchState struct {
	client     mixerpb.MixerClient
	connection *grpc.ClientConn
	gs         *grpc.Server
	gp         *pool.GoroutinePool
	s          *grpcServer
}

func (bs *benchState) createGRPCServer() (string, error) {
	// get the network stuff setup
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return "", err
	}

	for _, s := range log.Scopes() {
		s.SetOutputLevel(log.NoneLevel)
	}

	// get everything wired up
	bs.gs = grpc.NewServer(grpc.MaxConcurrentStreams(256), grpc.MaxRecvMsgSize(1024*1024))

	bs.gp = pool.NewGoroutinePool(32, false)
	bs.gp.AddWorkers(32)

	ms := NewGRPCServer(bs, bs.gp, nil, loadshedding.NewThrottler(loadshedding.DefaultOptions()))
	bs.s = ms.(*grpcServer)
	mixerpb.RegisterMixerServer(bs.gs, bs.s)

	go func() {
		_ = bs.gs.Serve(listener)
	}()

	return listener.Addr().String(), nil
}

func (bs *benchState) deleteGRPCServer() {
	bs.gs.GracefulStop()
	_ = bs.gp.Close()
}

func (bs *benchState) createAPIClient(dial string) error {
	var err error
	if bs.connection, err = grpc.Dial(dial, grpc.WithInsecure()); err != nil {
		return err
	}

	bs.client = mixerpb.NewMixerClient(bs.connection)
	return nil
}

func (bs *benchState) deleteAPIClient() {
	_ = bs.connection.Close()

	bs.client = nil
	bs.connection = nil
}

func prepBenchState() (*benchState, error) {
	bs := &benchState{}
	dial, err := bs.createGRPCServer()
	if err != nil {
		return nil, err
	}

	if err = bs.createAPIClient(dial); err != nil {
		bs.deleteGRPCServer()
		return nil, err
	}

	return bs, nil
}

func (bs *benchState) cleanupBenchState() {
	bs.deleteAPIClient()
	bs.deleteGRPCServer()
}

func (bs *benchState) Preprocess(ctx context.Context, requestBag attribute.Bag, responseBag *attribute.MutableBag) error {
	return nil
}

func (bs *benchState) Check(ctx context.Context, bag attribute.Bag) (adapter.CheckResult, error) {
	result := adapter.CheckResult{
		Status: status.OK,
	}
	return result, nil
}

func (bs *benchState) GetReporter(ctx context.Context) dispatcher.Reporter {
	return bs
}

func (bs *benchState) Report(bag attribute.Bag) error {
	return nil
}

func (bs *benchState) Flush() error {
	return nil
}

func (bs *benchState) Done() {
}

func (bs *benchState) Quota(ctx context.Context, requestBag attribute.Bag,
	qma dispatcher.QuotaMethodArgs) (adapter.QuotaResult, error) {

	qr := adapter.QuotaResult{
		Status: status.OK,
		Amount: 42,
	}
	return qr, nil
}

func BenchmarkAPI_Unary_GlobalDict_NoCompress(b *testing.B) {
	unaryBench(b, false, true)
}

func BenchmarkAPI_Unary_GlobalDict_Compress(b *testing.B) {
	unaryBench(b, true, true)
}

func BenchmarkAPI_Unary_NoGlobalDict_NoCompress(b *testing.B) {
	unaryBench(b, false, false)
}

func BenchmarkAPI_Unary_NoGlobalDict_Compress(b *testing.B) {
	unaryBench(b, true, false)
}

func unaryBench(b *testing.B, grpcCompression, useGlobalDict bool) {
	bs, err := prepBenchState()
	if err != nil {
		b.Fatalf("unable to prep test state %v", err)
	}
	defer bs.cleanupBenchState()

	globalDict := getGlobalDict()
	revGlobalDict := make(map[string]int32, len(globalDict))
	for k, v := range globalDict {
		revGlobalDict[v] = int32(k)
	}

	bag := attribute.GetMutableBag(nil)
	uuid := []byte("0143c90e-8515-4f00-b1d8-92f6ea666073")

	wg := sync.WaitGroup{}
	gp := pool.NewGoroutinePool(32, false)
	gp.AddWorkers(32)

	var opts []grpc.CallOption
	if grpcCompression {
		opts = append(opts, grpc.UseCompressor("gzip"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		uuid[4] = byte(i)
		setRequestAttrs(bag, uuid)

		request := &mixerpb.CheckRequest{}

		if useGlobalDict {
			attr.ToProto(bag, &request.Attributes, revGlobalDict, len(revGlobalDict))
		} else {
			attr.ToProto(bag, &request.Attributes, nil, 0)
		}

		wg.Add(1)
		gp.ScheduleWork(func(_ interface{}) {
			if _, err := bs.client.Check(context.Background(), request, opts...); err != nil {
				b.Errorf("Check2 failed with %v", err)
			}
			wg.Done()
		}, nil)
	}

	// wait for all the async work to be done
	wg.Wait()
}

func getGlobalDict() []string {
	return []string{
		"request.headers",
		":authority",
		":method", "GET",
		":path",
		"accept-encoding", "gzip",
		"content-length", "0",
		"user-agent", "Go-http-client/1.1",
		"x-forwarded-proto", "http",
		"x-request-id",
		"request.host",
		"request.path",
		"request.size",
		"request.time",
		"response.headers",
		":status", "200",
		"content-length",
		"content-type", "text/plain; charset=utf-8",
		"date",
		"server", "envoy",
		"x-envoy-upstream-service-time",
		"response.http.code",
		"response.duration",
		"response.size",
		"response.time",
		"source.namespace",
		"source.uid",
		"destination.namespace",
		"destination.uid",
	}
}

func setRequestAttrs(bag *attribute.MutableBag, uuid []byte) {
	bag.Set("request.headers", attribute.WrapStringMap(map[string]string{
		":authority":        "localhost:27070",
		":method":           "GET",
		":path":             "/echo",
		"accept-encoding":   "gzip",
		"content-length":    "0",
		"user-agent":        "Go-http-client/1.1",
		"x-forwarded-proto": "http",
		"x-request-id":      string(uuid),
	}))
	bag.Set("request.host", "localhost:27070")
	bag.Set("request.path", "/echo")
	bag.Set("request.size", int64(0))
	bag.Set("request.time", time.Now())
	bag.Set("response.headers", attribute.WrapStringMap(map[string]string{
		":status":                       "200",
		"content-length":                "0",
		"content-type":                  "text/plain; charset=utf-8",
		"date":                          time.Now().String(),
		"server":                        "envoy",
		"x-envoy-upstream-service-time": "0",
	}))
	bag.Set("response.http.code", int64(200))
	bag.Set("response.duration", time.Duration(50)*time.Millisecond)
	bag.Set("response.size", int64(64))
	bag.Set("response.time", time.Now())
	bag.Set("source.namespace", "XYZ11")
	bag.Set("source.uid", "POD11")
	bag.Set("destination.namespace", "XYZ222")
	bag.Set("destination.uid", "POD222")
}
