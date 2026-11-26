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

package bootstrap

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	reflectionpb "google.golang.org/grpc/reflection/grpc_reflection_v1"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/util/assert"
)

// Components must not be stopped while a gRPC stream is still open: they tear down state the
// stream is served from.
func TestComponentsStopAfterGrpc(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	grpcAddr := l.Addr().String()
	assert.NoError(t, l.Close())

	const shutdownDuration = 500 * time.Millisecond
	args := NewPilotArgs(func(p *PilotArgs) {
		p.Namespace = "istio-system"
		p.ServerOptions = DiscoveryServerOptions{
			HTTPAddr:       ":0",
			MonitoringAddr: ":0",
			GRPCAddr:       grpcAddr,
		}
		p.RegistryOptions = RegistryOptions{FileDir: t.TempDir()}
		p.ShutdownDuration = shutdownDuration
	})
	s, err := NewServer(args, func(s *Server) {
		s.kubeClient = kube.NewFakeClient()
	})
	assert.NoError(t, err)

	componentStopped := make(chan time.Time, 1)
	s.addStartFunc("test", func(stop <-chan struct{}) error {
		go func() {
			<-stop
			componentStopped <- time.Now()
		}()
		return nil
	})
	stop := make(chan struct{})
	assert.NoError(t, s.Start(stop))

	// Hold a stream open, so the gRPC server can only stop by reaching the shutdown duration.
	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	assert.NoError(t, err)
	defer conn.Close()
	stream, err := reflectionpb.NewServerReflectionClient(conn).ServerReflectionInfo(context.Background())
	assert.NoError(t, err)
	assert.NoError(t, stream.Send(&reflectionpb.ServerReflectionRequest{
		MessageRequest: &reflectionpb.ServerReflectionRequest_ListServices{},
	}))
	_, err = stream.Recv()
	assert.NoError(t, err)

	stopped := time.Now()
	close(stop)
	select {
	case at := <-componentStopped:
		if d := at.Sub(stopped); d < shutdownDuration {
			t.Fatalf("components stopped after %v, while the gRPC server was still serving", d)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("components were never stopped")
	}
	_, err = stream.Recv()
	assert.Equal(t, err != nil, true, fmt.Sprintf("stream still open: %v", err))
	s.WaitUntilCompletion()
}
