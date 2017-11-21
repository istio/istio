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

package mock

import (
	"net"

	"google.golang.org/grpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/cmd/server/cmd"
	"istio.io/istio/mixer/cmd/shared"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/template"
)

type Server struct {
	addr         string
	mixerContext *cmd.ServerContext
	shutdown     chan struct{}
}

// Args includes the required args to initialize Mixer server.
type Args struct {
	MixerServerAddr               string
	ConfigStoreURL                string
	ConfigStore2URL               string
	ConfigDefaultNamespace        string
	ConfigIdentityAttribute       string
	ConfigIdentityAttributeDomain string
}

// NewServer creates a Server instance.
func NewServer(args *Args, info map[string]template.Info, adapters []adapter.InfoFn) (*Server, error) {
	lis, err := net.Listen("tcp", args.MixerServerAddr)
	if err != nil {
		return nil, err
	}

	context := cmd.SetupTestServer(info, adapters, []adapter.RegisterFn{}, args.ConfigStoreURL, args.ConfigStore2URL,
		args.ConfigDefaultNamespace, args.ConfigIdentityAttribute, args.ConfigIdentityAttributeDomain)
	shutdown := make(chan struct{})

	go func() {
		shared.Printf("Start test Mixer on:%v\n", lis.Addr())
		{
			defer context.GP.Close()
			defer context.AdapterGP.Close()
			if err := context.Server.Serve(lis); err != nil {
				shared.Printf("Mixer Shutdown: %v\n", err)
			}
		}
		shutdown <- struct{}{}
	}()

	return &Server{
		addr:         lis.Addr().String(),
		mixerContext: context,
		shutdown:     shutdown,
	}, nil
}

// CreateClient returns a Mixer Grpc client.
func (s *Server) CreateClient() (mixerpb.MixerClient, *grpc.ClientConn, error) {
	conn, err := grpc.Dial(s.addr, grpc.WithInsecure())
	if err != nil {
		return nil, nil, err
	}

	return mixerpb.NewMixerClient(conn), conn, nil
}

// Close cleans up Server.
func (s *Server) Close() error {
	s.mixerContext.Server.GracefulStop()
	<-s.shutdown
	close(s.shutdown)
	s.mixerContext = nil
	return nil
}

// GetAttrBag creates Attributes proto.
func GetAttrBag(attrs map[string]interface{}, identityAttr, identityAttrDomain string) mixerpb.CompressedAttributes {
	requestBag := attribute.GetMutableBag(nil)
	requestBag.Set(identityAttr, identityAttrDomain)
	for k, v := range attrs {
		requestBag.Set(k, v)
	}

	var attrProto mixerpb.CompressedAttributes
	requestBag.ToProto(&attrProto, nil, 0)
	return attrProto
}
