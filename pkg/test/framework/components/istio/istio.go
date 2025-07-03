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
	"net/netip"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/cleanup"
	"istio.io/istio/pkg/test/scopes"
)

// Instance represents a deployed Istio instance
type Instance interface {
	resource.Resource

	Settings() Config
	// Ingresses returns all ingresses for "istio-ingressgateway" in each cluster.
	Ingresses() ingress.Instances
	// IngressFor returns an ingress used for reaching workloads in the given cluster.
	// The ingress's service name will be "istio-ingressgateway" and the istio label will be "ingressgateway".
	IngressFor(cluster cluster.Cluster) ingress.Instance
	// EastWestGatewayFor returns an ingress used for east-west traffic and accessing the control plane
	// from outside of the cluster.
	EastWestGatewayFor(cluster cluster.Cluster) ingress.Instance
	// EastWestGatewayForAmbient returns an ingress used for east-west traffic and accessing the control plane
	// from outside of the cluster when in ambient mode.
	EastWestGatewayForAmbient(cluster cluster.Cluster) ingress.Instance
	// CustomIngressFor returns an ingress with a specific service name and "istio" label used for reaching workloads
	// in the given cluster.
	CustomIngressFor(cluster cluster.Cluster, service types.NamespacedName, istioLabel string) ingress.Instance

	// RemoteDiscoveryAddressFor returns the external address of the discovery server that controls
	// the given cluster. This allows access to the discovery server from
	// outside its cluster.
	RemoteDiscoveryAddressFor(cluster cluster.Cluster) (netip.AddrPort, error)
	// CreateRemoteSecret on the cluster with the given options.
	CreateRemoteSecret(ctx resource.Context, c cluster.Cluster, opts ...string) (string, error)
	// InternalDiscoveryAddressFor returns an internal (port-forwarded) address for an Istiod instance in the
	// cluster.
	InternalDiscoveryAddressFor(cluster cluster.Cluster) (string, error)

	// Return POD IPs for the pod with the specified label in the specified namespace
	PodIPsFor(cluster cluster.Cluster, namespace string, label string) ([]corev1.PodIP, error)

	// MeshConfig used by the Istio installation.
	MeshConfig() (*meshconfig.MeshConfig, error)
	MeshConfigOrFail(test.Failer) *meshconfig.MeshConfig
	// UpdateMeshConfig used by the Istio installation.
	UpdateMeshConfig(resource.Context, func(*meshconfig.MeshConfig) error, cleanup.Strategy) error
	UpdateMeshConfigOrFail(resource.ContextFailer, func(*meshconfig.MeshConfig) error, cleanup.Strategy)
	// PatchMeshConfig with the given patch yaml.
	PatchMeshConfig(resource.Context, string) error
	PatchMeshConfigOrFail(resource.ContextFailer, string)
	UpdateInjectionConfig(resource.Context, func(*inject.Config) error, cleanup.Strategy) error
	InjectionConfig() (*inject.Config, error)
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
func GetOrFail(t resource.ContextFailer) Instance {
	t.Helper()
	i, err := Get(t)
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

		t0 := time.Now()
		scopes.Framework.Infof("=== BEGIN: Deploy Istio [Suite=%s] ===", ctx.Settings().TestID)

		ins, err := newKube(ctx, cfg)
		if err != nil {
			scopes.Framework.Infof("=== FAILED: Deploy Istio in %v [Suite=%s] ===", time.Since(t0), ctx.Settings().TestID)
			return err
		}

		if i != nil {
			*i = ins
		}
		scopes.Framework.Infof("=== SUCCEEDED: Deploy Istio in %v [Suite=%s]===", time.Since(t0), ctx.Settings().TestID)
		return nil
	}
}
