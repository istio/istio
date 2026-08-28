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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube"
)

const (
	// defaultExpirationSeconds is how long-lived a token to request (an hour)
	defaultExpirationSeconds = 60 * 60
)

// Audience to create tokens for
var tokenAudiences = []string{"istio-ca"}

// GetXdsResponse opens a gRPC connection to opts.xds and waits for a single response
func GetXdsResponse(dr *discovery.DiscoveryRequest, ns string, serviceAccount string, opts clioptions.CentralControlPlaneOptions,
	grpcOpts []grpc.DialOption,
) (*discovery.DiscoveryResponse, error) {
	adscConn, err := adsc.NewWithBackoffPolicy(opts.Xds, &adsc.ADSConfig{
		Config: adsc.Config{
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
		},
	}, nil)
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
	tlsCfg, err := tlsConfig(ctx, opts, ns, kubeClient)
	if err != nil {
		return nil, err
	}
	return []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithPerRPCCredentials(k8sCreds),
	}, nil
}

// tlsConfig verifies istiod against the root in the istio-ca-root-cert configmap unless --insecure is set.
func tlsConfig(ctx context.Context, opts clioptions.CentralControlPlaneOptions, ns string, kubeClient kube.CLIClient) (*tls.Config, error) {
	cfg := &adsc.Config{
		Address:            opts.Xds,
		XDSSAN:             opts.XDSSAN,
		InsecureSkipVerify: opts.InsecureSkipVerify,
	}
	if !opts.InsecureSkipVerify {
		cm, err := kubeClient.Kube().CoreV1().ConfigMaps(ns).Get(ctx, controller.CACertNamespaceConfigMap, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to read the XDS root cert from configmap %s/%s (use --cert-dir, or --insecure to skip verification): %w",
				ns, controller.CACertNamespaceConfigMap, err)
		}
		cfg.XDSRootCA = []byte(cm.Data[constants.CACertNamespaceConfigMapDataName])
	}
	return adsc.TLSConfig(cfg)
}
