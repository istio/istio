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

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/oauth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/kube"
)

type meshAuthCredentials struct {
	k8sCreds credentials.PerRPCCredentials
	gcpCreds credentials.PerRPCCredentials
	project  string
}

func (c *meshAuthCredentials) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	ret := map[string]string{
		"x-goog-user-project": c.project,
	}
	if err := updateAuthHdrs(ctx, uri, "k8s", c.k8sCreds, ret, "x-mesh-authorization"); err != nil {
		return nil, err
	}
	if err := updateAuthHdrs(ctx, uri, "gcp", c.gcpCreds, ret, "authorization"); err != nil {
		return nil, err
	}
	return ret, nil
}

func (*meshAuthCredentials) RequireTransportSecurity() bool {
	return true
}

func updateAuthHdrs(ctx context.Context, uri []string, kind string, creds credentials.PerRPCCredentials, dst map[string]string, dstHdr string) error {
	ret, err := creds.GetRequestMetadata(ctx, uri...)
	if err != nil {
		return err
	}
	for k, v := range ret {
		if !strings.EqualFold(k, "authorization") {
			if _, ok := dst[k]; ok {
				return fmt.Errorf("underlying %s credentials contain a %s header which is already present in the combined credentials", kind, k)
			}
			dst[k] = v
		} else {
			dst[dstHdr] = v
		}
	}
	return nil
}

type hubMembership struct {
	WorkloadIdentityPool string
}

func getHubMembership(ctx context.Context, exClient kube.ExtendedClient) (*hubMembership, error) {
	client := exClient.Dynamic()
	gvr := schema.GroupVersionResource{
		Group:    "hub.gke.io",
		Version:  "v1",
		Resource: "memberships",
	}
	u, err := client.Resource(gvr).Get(ctx, "membership", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	spec, ok := u.Object["spec"].(map[string]any)
	if !ok {
		return nil, errors.New(`field "spec" is not a map`)
	}
	var mem hubMembership
	mem.WorkloadIdentityPool, ok = spec["workload_identity_pool"].(string)
	if !ok {
		return nil, errors.New(`field "spec.workload_identity_pool" is not a string`)
	}
	return &mem, nil
}

func mcpDialOptions(ctx context.Context, gcpProject string, k8sCreds credentials.PerRPCCredentials) ([]grpc.DialOption, error) {
	systemRoots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("failed to get system cert pool: %w", err)
	}
	gcpCreds, err := oauth.NewApplicationDefault(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get application default credentials: %w", err)
	}

	return []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			RootCAs: systemRoots,
		})),
		grpc.WithPerRPCCredentials(&meshAuthCredentials{
			k8sCreds: k8sCreds,
			gcpCreds: gcpCreds,
			project:  gcpProject,
		}),
	}, nil
}
