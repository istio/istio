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

package xds

// xds uses ADSC to call XDS

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/adsc2"
	"istio.io/istio/pkg/kube"
)

const (
	// defaultExpirationSeconds is how long-lived a token to request (an hour)
	defaultExpirationSeconds = 60 * 60
)

// Audience to create tokens for
var tokenAudiences = []string{"istio-ca"}

// GetXdsResponse opens a gRPC connection to opts.xds and waits for a single response
func GetXdsResponse(dr *discovery.DeltaDiscoveryRequest, ns, serviceAccount string, opts clioptions.CentralControlPlaneOptions,
	grpcOpts []grpc.DialOption,
) (*discovery.DeltaDiscoveryResponse, error) {
	waitRes := make(chan *anypb.Any, 10)

	handlers := make([]adsc2.Option, 0)
	count := 0
	handlers = append(handlers, adsc2.RegisterType(dr.TypeUrl, func(ctx adsc2.HandlerContext, res proto.Message, event adsc2.Event) {
		waitRes <- res.(*anypb.Any)
		if count == 0 {
			count = ctx.ResourceCount()
		}
	}))
	for _, resource := range dr.ResourceNamesSubscribe {
		handlers = append(handlers, adsc2.WatchType(dr.TypeUrl, resource))
	}
	if len(dr.ResourceNamesSubscribe) == 0 {
		handlers = append(handlers, adsc2.WatchType(dr.TypeUrl, "*"))
	}

	adsClient := adsc2.New(&adsc2.Config{
		Address: opts.Xds,
		Meta: model.NodeMetadata{
			Generator:      "event",
			ServiceAccount: serviceAccount,
			Namespace:      ns,
			CloudrunAddr:   opts.IstiodAddr,
		}.ToStruct(),
		CertDir:            opts.CertDir,
		InsecureSkipVerify: opts.InsecureSkipVerify,
		XDSSAN:             opts.XDSSAN,
		GrpcOpts:           grpcOpts,
	}, handlers...)

	// Start ADS client
	err := adsClient.Run(context.Background())
	if err != nil {
		fmt.Printf("ADSC: failed running %v\n", err)
		return nil, err
	}

	results := make([]*anypb.Any, 0)

	for res := range waitRes {
		results = append(results, res)
		if count == len(results) {
			break
		}
	}

	adsClient.Close()

	resources := make([]*discovery.Resource, 0)
	for _, result := range results {
		resources = append(resources, &discovery.Resource{Resource: result})
	}
	return &discovery.DeltaDiscoveryResponse{
		Resources: resources,
	}, nil
}

// DialOptions constructs gRPC dial options from command line configuration
func DialOptions(opts clioptions.CentralControlPlaneOptions,
	ns, serviceAccount string, kubeClient kube.CLIClient,
) ([]grpc.DialOption, error) {
	ctx := context.TODO()
	// If we are using the insecure 15010 don't bother getting a token
	if opts.Plaintext || opts.CertDir != "" {
		return make([]grpc.DialOption, 0), nil
	}
	// Use bearer token
	aud := tokenAudiences
	isMCP := strings.HasSuffix(opts.Xds, ".googleapis.com") || strings.HasSuffix(opts.Xds, ".googleapis.com:443")
	if isMCP {
		// Special credentials handling when using ASM Managed Control Plane.
		mem, err := getHubMembership(ctx, kubeClient)
		if err != nil {
			return nil, fmt.Errorf("failed to query Hub membership: %w", err)
		}
		aud = []string{mem.WorkloadIdentityPool}
	}
	k8sCreds, err := kubeClient.CreatePerRPCCredentials(ctx, ns, serviceAccount, aud, defaultExpirationSeconds)
	if err != nil {
		return nil, fmt.Errorf("failed to create RPC credentials for \"%s.%s\": %w", serviceAccount, ns, err)
	}
	if isMCP {
		return mcpDialOptions(ctx, opts.GCPProject, k8sCreds)
	}
	return []grpc.DialOption{
		// nolint: gosec
		// Only runs over istioctl experimental
		// TODO: https://github.com/istio/istio/issues/41937
		grpc.WithTransportCredentials(credentials.NewTLS(
			&tls.Config{
				// Always skip verifying, because without it we always get "certificate signed by unknown authority".
				// We don't set the XDSSAN for the same reason.
				InsecureSkipVerify: true,
			})),
		grpc.WithPerRPCCredentials(k8sCreds),
	}, nil
}
