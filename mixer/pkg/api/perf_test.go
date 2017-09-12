// Copyright 2017 Istio Authors
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

	rpc "github.com/googleapis/googleapis/google/rpc"
	"google.golang.org/grpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/pool"
	"istio.io/mixer/pkg/status"
)

type benchState struct {
	client       mixerpb.MixerClient
	connection   *grpc.ClientConn
	gs           *grpc.Server
	gp           *pool.GoroutinePool
	s            *grpcServer
	delayOnClose bool
}

func (bs *benchState) createGRPCServer(grpcCompression bool) (string, error) {
	// get the network stuff setup
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return "", err
	}

	var grpcOptions []grpc.ServerOption
	grpcOptions = append(grpcOptions, grpc.MaxConcurrentStreams(256))
	grpcOptions = append(grpcOptions, grpc.MaxMsgSize(1024*1024))

	if grpcCompression {
		grpcOptions = append(grpcOptions, grpc.RPCCompressor(grpc.NewGZIPCompressor()))
		grpcOptions = append(grpcOptions, grpc.RPCDecompressor(grpc.NewGZIPDecompressor()))
	}

	// get everything wired up
	bs.gs = grpc.NewServer(grpcOptions...)

	bs.gp = pool.NewGoroutinePool(32, false)
	bs.gp.AddWorkers(32)

	ms := NewGRPCServer(bs, nil, bs.gp)
	bs.s = ms.(*grpcServer)
	mixerpb.RegisterMixerServer(bs.gs, bs.s)

	go func() {
		_ = bs.gs.Serve(listener)
	}()

	return listener.Addr().String(), nil
}

func (bs *benchState) deleteGRPCServer() {
	bs.gs.GracefulStop()
	bs.gp.Close()
}

func (bs *benchState) createAPIClient(dial string, grpcCompression bool) error {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())

	if grpcCompression {
		opts = append(opts, grpc.WithCompressor(grpc.NewGZIPCompressor()))
		opts = append(opts, grpc.WithDecompressor(grpc.NewGZIPDecompressor()))
	}

	var err error
	if bs.connection, err = grpc.Dial(dial, opts...); err != nil {
		return err
	}

	bs.delayOnClose = true
	bs.client = mixerpb.NewMixerClient(bs.connection)
	return nil
}

func (bs *benchState) deleteAPIClient() {
	_ = bs.connection.Close()

	if bs.delayOnClose {
		// TODO: This is to compensate for this bug: https://github.com/grpc/grpc-go/issues/1059
		//       Remove this delay once that bug is fixed.
		time.Sleep(100 * time.Millisecond)
	}

	bs.client = nil
	bs.connection = nil
}

func prepBenchState(grpcCompression bool) (*benchState, error) {
	bs := &benchState{}
	dial, err := bs.createGRPCServer(grpcCompression)
	if err != nil {
		return nil, err
	}

	if err = bs.createAPIClient(dial, grpcCompression); err != nil {
		bs.deleteGRPCServer()
		return nil, err
	}

	return bs, nil
}

func (bs *benchState) cleanupBenchState() {
	bs.deleteAPIClient()
	bs.deleteGRPCServer()
}

func (bs *benchState) Check(ctx context.Context, bag attribute.Bag, output *attribute.MutableBag) rpc.Status {
	output.Set("kubernetes.pod", "a pod name")
	output.Set("kubernetes.service", "a service name")
	output.Set("kubernetes.id", int64(123456))
	output.Set("kubernetes.time", time.Now())
	return status.OK
}

func (bs *benchState) Report(_ context.Context, _ attribute.Bag) rpc.Status {
	return status.WithPermissionDenied("Not Implemented")
}

func (bs *benchState) Quota(ctx context.Context, requestBag attribute.Bag,
	qma *aspect.QuotaMethodArgs) (*aspect.QuotaMethodResp, rpc.Status) {

	qmr := &aspect.QuotaMethodResp{Amount: 42}
	return qmr, status.OK
}

func (bs *benchState) Preprocess(ctx context.Context, requestBag attribute.Bag, responseBag *attribute.MutableBag) rpc.Status {
	return status.OK
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
	bs, err := prepBenchState(grpcCompression)
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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		uuid[4] = byte(i)
		setRequestAttrs(bag, uuid)

		request := &mixerpb.CheckRequest{}

		if useGlobalDict {
			bag.ToProto(&request.Attributes, revGlobalDict, len(revGlobalDict))
		} else {
			bag.ToProto(&request.Attributes, nil, 0)
		}

		wg.Add(1)
		gp.ScheduleWork(func() {
			if _, err := bs.client.Check(context.Background(), request); err != nil {
				b.Errorf("Check2 failed with %v", err)
			}
			wg.Done()
		})
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
		"target.namespace",
		"target.uid",
	}
}

func setRequestAttrs(bag *attribute.MutableBag, uuid []byte) {
	bag.Set("request.headers", map[string]string{
		":authority":        "localhost:27070",
		":method":           "GET",
		":path":             "/echo",
		"accept-encoding":   "gzip",
		"content-length":    "0",
		"user-agent":        "Go-http-client/1.1",
		"x-forwarded-proto": "http",
		"x-request-id":      string(uuid),
	})
	bag.Set("request.host", "localhost:27070")
	bag.Set("request.path", "/echo")
	bag.Set("request.size", int64(0))
	bag.Set("request.time", time.Now())
	bag.Set("response.headers", map[string]string{
		":status":                       "200",
		"content-length":                "0",
		"content-type":                  "text/plain; charset=utf-8",
		"date":                          time.Now().String(),
		"server":                        "envoy",
		"x-envoy-upstream-service-time": "0",
	})
	bag.Set("response.http.code", int64(200))
	bag.Set("response.duration", time.Duration(50)*time.Millisecond)
	bag.Set("response.size", int64(64))
	bag.Set("response.time", time.Now())
	bag.Set("source.namespace", "XYZ11")
	bag.Set("source.uid", "POD11")
	bag.Set("target.namespace", "XYZ222")
	bag.Set("target.uid", "POD222")
}
