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
	"strconv"
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"istio.io/api/mesh/v1alpha1"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
)

const (
	// defaultExpirationSeconds is how long-lived a token to request (an hour)
	defaultExpirationSeconds = 60 * 60

	// Service account to create tokens in
	tokenServiceAccount = "default"
	tokenNamespace      = "default"
)

// Audience to create tokens for
var tokenAudiences = []string{"istio-ca"}

// GetXdsResponse opens a gRPC connection to opts.xds and waits for a single response
func GetXdsResponse(dr *xdsapi.DiscoveryRequest, opts *clioptions.CentralControlPlaneOptions, grpcOpts []grpc.DialOption) (*xdsapi.DiscoveryResponse, error) {
	adscConn, err := adsc.New(opts.Xds, &adsc.Config{
		Meta: model.NodeMetadata{
			Generator: "event",
		}.ToStruct(),
		CertDir:            opts.CertDir,
		InsecureSkipVerify: opts.InsecureSkipVerify,
		XDSSAN:             opts.XDSSAN,
		GrpcOpts:           grpcOpts,
	})
	if err != nil {
		return nil, fmt.Errorf("could not dial: %w", err)
	}
	err = adscConn.Run()
	if err != nil {
		return nil, fmt.Errorf("ADSC: failed running %v", err)
	}

	err = adscConn.Send(dr)
	if err != nil {
		return nil, err
	}

	response, err := adscConn.WaitVersion(opts.Timeout, dr.TypeUrl, "")
	return response, err
}

// DialOptions constructs gRPC dial options from command line configuration
func DialOptions(opts *clioptions.CentralControlPlaneOptions, kubeClient kube.ExtendedClient) ([]grpc.DialOption, error) {
	// If we are using the insecure 15010 don't bother getting a token
	if opts.Plaintext || opts.CertDir != "" {
		return make([]grpc.DialOption, 0), nil
	}

	// Use bearer token
	supplier, err := kubeClient.CreatePerRPCCredentials(context.TODO(), tokenNamespace, tokenServiceAccount, tokenAudiences, defaultExpirationSeconds)
	if err != nil {
		return nil, err
	}
	return []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(
			&tls.Config{
				// Always skip verifying, because without it we always get "certificate signed by unknown authority".
				// We don't se the XDSSAN for the same reason.
				InsecureSkipVerify: true,
			})),
		grpc.WithPerRPCCredentials(supplier),
	}, err
}

// ApplyXdsFlagDefaults uses values from the injector config for unset flags needed to contact istiod
func ApplyXdsFlagDefaults(centralOpts *clioptions.CentralControlPlaneOptions, istioNamespace, revision, meshConfigFile string, kubeClient kube.Client) error {
	// If the user did not specify any XDS address or Istiod selector, get them from injector config
	if centralOpts.Xds == "" && centralOpts.XdsPodLabel == "" && centralOpts.XdsPodPort == 0 {
		var meshConfig *v1alpha1.MeshConfig
		var err error
		if meshConfigFile != "" {
			if meshConfig, err = mesh.ReadMeshConfig(meshConfigFile); err != nil {
				return err
			}
		} else {
			if meshConfig, err = clioptions.GetMeshConfigFromConfigMap(kubeClient, istioNamespace, revision); err != nil {
				return err
			}
		}

		if localClusterAddress(meshConfig.DefaultConfig.DiscoveryAddress) {
			// The discovery address looks local.  Default istioctl to using local Istiod via K8s
			centralOpts.XdsPodPort = xdsPort(meshConfig.DefaultConfig.DiscoveryAddress)
		} else {
			// The discovery address looks non-local.  Use it as default for istioctl.
			centralOpts.Xds = meshConfig.DefaultConfig.DiscoveryAddress
		}
	}

	return nil
}

// Returns true if the XDS address is <host>.svc:<port>
func localClusterAddress(discoveryAddress string) bool {
	discHost := strings.Split(discoveryAddress, ":")[0]
	return strings.HasSuffix(discHost, ".svc")
}

// Returns the XDS port from an address "host:port", defaulting to 15012 if not present
func xdsPort(discoveryAddress string) int {
	parts := strings.Split(discoveryAddress, ":")
	if len(parts) == 2 {
		port, err := strconv.Atoi(parts[1])
		if err != nil {
			return port
		}
	}
	return 15012
}
