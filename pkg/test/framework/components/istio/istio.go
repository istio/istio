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
	"regexp"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/structpb"
	"k8s.io/apimachinery/pkg/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/cleanup"
	"istio.io/istio/pkg/test/scopes"
)

// OperatorValues is the map of the values from the installed operator yaml.
type OperatorValues map[string]*structpb.Value

// This regular expression matches list object index selection expression such as
// abc[100], Tba_a[0].
var listObjRex = regexp.MustCompile(`^([a-zA-Z]?[a-z_A-Z\d]*)\[([ ]*[\d]+)[ ]*\]$`)

func getConfigValue(path []string, val map[string]*structpb.Value) *structpb.Value {
	retVal := structpb.NewNullValue()
	if len(path) > 0 {
		match := listObjRex.FindStringSubmatch(path[0])
		// valid list index
		switch len(match) {
		case 0: // does not match list object selection, should be name of a field, should be struct value
			thisVal := val[path[0]]
			// If it is a struct and looking for more down the path
			if thisVal.GetStructValue() != nil && len(path) > 1 {
				return getConfigValue(path[1:], thisVal.GetStructValue().Fields)
			}
			retVal = thisVal
		case 3: // match somthing like aaa[100]
			thisVal := val[match[1]]
			// If it is a list and looking for more down the path
			if thisVal.GetListValue() != nil && len(path) > 1 {
				index, _ := strconv.Atoi(match[2])
				return getConfigValue(path[1:], thisVal.GetListValue().Values[index].GetStructValue().Fields)
			}
			retVal = thisVal
		}
	}
	return retVal
}

// GetConfigValue returns a structpb value from a structpb map by
// using a dotted path such as `pilot.env.LOCAL_CLUSTER_SECRET_WATCHER`.
func (v OperatorValues) GetConfigValue(path string) *structpb.Value {
	return getConfigValue(strings.Split(path, "."), v)
}

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
	// CustomIngressFor returns an ingress with a specific service name and "istio" label used for reaching workloads
	// in the given cluster.
	CustomIngressFor(cluster cluster.Cluster, service types.NamespacedName, istioLabel string) ingress.Instance

	// RemoteDiscoveryAddressFor returns the external address of the discovery server that controls
	// the given cluster. This allows access to the discovery server from
	// outside its cluster.
	RemoteDiscoveryAddressFor(cluster cluster.Cluster) (net.TCPAddr, error)
	// CreateRemoteSecret on the cluster with the given options.
	CreateRemoteSecret(ctx resource.Context, c cluster.Cluster, opts ...string) (string, error)
	// Values returns the operator values for the installed control plane.
	Values() (OperatorValues, error)
	ValuesOrFail(test.Failer) OperatorValues
	// MeshConfig used by the Istio installation.
	MeshConfig() (*meshconfig.MeshConfig, error)
	MeshConfigOrFail(test.Failer) *meshconfig.MeshConfig
	// UpdateMeshConfig used by the Istio installation.
	UpdateMeshConfig(resource.Context, func(*meshconfig.MeshConfig) error, cleanup.Strategy) error
	UpdateMeshConfigOrFail(resource.Context, test.Failer, func(*meshconfig.MeshConfig) error, cleanup.Strategy)
	// PatchMeshConfig with the given patch yaml.
	PatchMeshConfig(resource.Context, string) error
	PatchMeshConfigOrFail(resource.Context, test.Failer, string)
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
