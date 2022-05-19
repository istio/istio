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

package istio

import (
	"net"
	"time"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

// Instance represents a deployed Istio instance
type Instance interface {
	resource.Resource

	// Ingresses returns all ingresses for "istio-ingressgateway" in each cluster.
	Ingresses() ingress.Instances
	// IngressFor returns an ingress used for reaching workloads in the given cluster.
	// The ingress's service name will be "istio-ingressgateway" and the istio label will be "ingressgateway".
	IngressFor(cluster cluster.Cluster) ingress.Instance
	// CustomIngressFor returns an ingress with a specific service name and "istio" label used for reaching workloads
	// in the given cluster.
	CustomIngressFor(cluster cluster.Cluster, serviceName, istioLabel string) ingress.Instance

	// RemoteDiscoveryAddressFor returns the external address of the discovery server that controls
	// the given cluster. This allows access to the discovery server from
	// outside its cluster.
	RemoteDiscoveryAddressFor(cluster cluster.Cluster) (net.TCPAddr, error)
	Settings() Config
}

// SetupConfigFn is a setup function that specifies the overrides of the configuration to deploy Istio.
type SetupConfigFn func(ctx resource.Context, cfg *Config)

// SetupContextFn is a setup function that uses Context for configuration.
type SetupContextFn func(ctx resource.Context) error

// Get returns the Istio component from the context. If there is none an error is returned.
func Get(ctx resource.Context) (Instance, error) {
	var i Instance
	if err := ctx.GetResource(&i); err != nil {
		return nil, err
	}
	return i, nil
}

// GetOrFail returns the Istio component from the context. If there is none the test is failed.
func GetOrFail(t test.Failer, ctx resource.Context) Instance {
	t.Helper()
	i, err := Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

// DefaultIngress returns the ingress installed in the default cluster. The ingress's service name
// will be "istio-ingressgateway" and the istio label will be "ingressgateway".
func DefaultIngress(ctx resource.Context) (ingress.Instance, error) {
	i, err := Get(ctx)
	if err != nil {
		return nil, err
	}
	return i.IngressFor(ctx.Clusters().Default()), nil
}

// DefaultIngressOrFail calls DefaultIngress and fails if an error is encountered.
func DefaultIngressOrFail(t test.Failer, ctx resource.Context) ingress.Instance {
	t.Helper()
	i, err := DefaultIngress(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

// Ingresses returns all ingresses for "istio-ingressgateway" in each cluster.
func Ingresses(ctx resource.Context) (ingress.Instances, error) {
	i, err := Get(ctx)
	if err != nil {
		return nil, err
	}
	return i.Ingresses(), nil
}

// IngressesOrFail calls Ingresses and fails if an error is encountered.
func IngressesOrFail(t test.Failer, ctx resource.Context) ingress.Instances {
	t.Helper()
	i, err := Ingresses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

// Setup is a setup function that will deploy Istio on Kubernetes environment
func Setup(i *Instance, cfn SetupConfigFn, ctxFns ...SetupContextFn) resource.SetupFn {
	return func(ctx resource.Context) error {
		cfg, err := DefaultConfig(ctx)
		if err != nil {
			return err
		}
		if cfn != nil {
			cfn(ctx, &cfg)
		}
		for _, ctxFn := range ctxFns {
			if ctxFn != nil {
				err := ctxFn(ctx)
				if err != nil {
					scopes.Framework.Infof("=== FAILED: context setup function [err=%v] ===", err)
					return err
				}
				scopes.Framework.Info("=== SUCCESS: context setup function ===")
			}
		}

		ins, err := Deploy(ctx, &cfg)
		if err != nil {
			return err
		}
		if i != nil {
			*i = ins
		}

		return nil
	}
}

// Deploy deploys (or attaches to) an Istio deployment and returns a handle. If cfg is nil, then DefaultConfig is used.
func Deploy(ctx resource.Context, cfg *Config) (Instance, error) {
	if cfg == nil {
		c, err := DefaultConfig(ctx)
		if err != nil {
			return nil, err
		}
		cfg = &c
	}

	t0 := time.Now()
	scopes.Framework.Infof("=== BEGIN: Deploy Istio [Suite=%s] ===", ctx.Settings().TestID)

	i, err := deploy(ctx, ctx.Environment().(*kube.Environment), *cfg)
	if err != nil {
		scopes.Framework.Infof("=== FAILED: Deploy Istio in %v [Suite=%s] ===", time.Since(t0), ctx.Settings().TestID)
	} else {
		scopes.Framework.Infof("=== SUCCEEDED: Deploy Istio in %v [Suite=%s]===", time.Since(t0), ctx.Settings().TestID)
	}
	return i, err
}
