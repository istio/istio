// Copyright Istio Authors.
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

package multicluster

import (
	"fmt"
	"io/ioutil"
	"sync"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/golang/sync/errgroup"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/mesh/v1alpha1"
	iop "istio.io/api/operator/v1alpha1"
	operatorV1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/validate"

	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/util/protomarshal"
)

// defaults the user can override
func defaultControlPlane() *operatorV1alpha1.IstioOperator {
	return &operatorV1alpha1.IstioOperator{
		Kind:       "IstioOperator",
		ApiVersion: "install.istio.io/v1alpha1",
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
		Spec: &iop.IstioOperatorSpec{
			Profile: "default",
		},
	}
}

// overlay configuration which will override user config.
func overlayIstioControlPlane(mesh *Mesh, current *Cluster, meshNetworks *v1alpha1.MeshNetworks) ( // nolint: interfacer
	*operatorV1alpha1.IstioOperator, error) {
	meshNetworksJSON, err := protomarshal.ToJSONMap(meshNetworks)
	if err != nil {
		return nil, err
	}
	untypedValues := map[string]interface{}{
		"global": map[string]interface{}{
			"meshID": mesh.meshID,
		},
	}
	typedValues := &operatorV1alpha1.Values{
		Gateways: &operatorV1alpha1.GatewaysConfig{
			IstioIngressgateway: &operatorV1alpha1.IngressGatewayConfig{
				Env: map[string]interface{}{
					"ISTIO_MESH_NETWORK": current.Network,
				},
			},
		},
		Global: &operatorV1alpha1.GlobalConfig{
			ControlPlaneSecurityEnabled: &types.BoolValue{Value: true},
			MeshNetworks:                meshNetworksJSON,
			MultiCluster: &operatorV1alpha1.MultiClusterConfig{
				ClusterName: current.clusterName,
			},
			Network: current.Network,
		},
	}

	typedValuesJSON, err := protomarshal.ToJSONMap(typedValues)
	if err != nil {
		return nil, err
	}

	return &operatorV1alpha1.IstioOperator{
		Kind:       "IstioOperator",
		ApiVersion: "install.istio.io/v1alpha1",
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
		Spec: &iop.IstioOperatorSpec{
			Values:            typedValuesJSON,
			UnvalidatedValues: untypedValues,
		},
	}, nil
}

func generateIstioControlPlane(mesh *Mesh, current *Cluster, meshNetworks *v1alpha1.MeshNetworks, from string) (string, error) { // nolint:interfacer
	var base *operatorV1alpha1.IstioOperator
	if from != "" {
		b, err := ioutil.ReadFile(from)
		if err != nil {
			return "", err
		}
		var user operatorV1alpha1.IstioOperator
		if err := util.UnmarshalWithJSONPB(string(b), &user, false); err != nil {
			return "", err
		}
		if errs := validate.CheckIstioOperatorSpec(user.Spec, false); len(errs) != 0 {
			return "", fmt.Errorf("source spec was not valid: %v", errs)
		}
		base = &user
	} else {
		base = defaultControlPlane()
	}

	overlay, err := overlayIstioControlPlane(mesh, current, meshNetworks)
	if err != nil {
		return "", err
	}

	baseYAML, err := util.MarshalWithJSONPB(base)
	if err != nil {
		return "", err
	}
	overlayYAML, err := util.MarshalWithJSONPB(overlay)
	if err != nil {
		return "", err
	}
	mergedYAML, err := util.OverlayYAML(baseYAML, overlayYAML)
	if err != nil {
		return "", err
	}

	return mergedYAML, nil
}

func waitForReadyGateways(env Environment, mesh *Mesh) error {
	env.Errorf("Waiting for ingress gateways to be ready\n")

	var wg errgroup.Group

	var notReadyMu sync.Mutex
	notReady := make(map[string]struct{})

	for uid := range mesh.clustersByClusterName {
		c := mesh.clustersByClusterName[uid]
		notReady[uid] = struct{}{}
		wg.Go(func() error {
			return env.Poll(1*time.Second, 5*time.Minute, func() (bool, error) {
				gateways := c.readIngressGateways()
				if len(gateways) > 0 {
					notReadyMu.Lock()
					delete(notReady, c.clusterName)
					notReadyMu.Unlock()
					return true, nil
				}
				return false, nil
			})
		})
	}
	if err := wg.Wait(); err != nil {
		clusters := make([]string, 0, len(notReady))
		for uid := range notReady {
			clusters = append(clusters, uid)
		}
		return fmt.Errorf("one or more clusters gateways were not ready: %v", clusters)
	}

	env.Errorf("Ingress gateways ready\n")
	return nil
}

func generateOutput(opt generateOptions, env Environment) error {
	mesh, err := meshFromFileDesc(opt.filename, env)
	if err != nil {
		return err
	}

	if opt.waitForGateways {
		if err := waitForReadyGateways(env, mesh); err != nil {
			return err
		}
	}

	context := opt.Context
	if context == "" {
		context = env.GetConfig().CurrentContext
	}
	cluster, ok := mesh.clustersByContext[context]
	if !ok {
		return fmt.Errorf("context %v not found", context)
	}

	meshNetwork, err := meshNetworkForCluster(env, mesh, cluster)
	if err != nil {
		return err
	}

	out, err := generateIstioControlPlane(mesh, cluster, meshNetwork, opt.from)
	if err != nil {
		return err
	}
	env.Printf("%v\n", out)

	return nil
}

func meshNetworkForCluster(env Environment, mesh *Mesh, current *Cluster) (*v1alpha1.MeshNetworks, error) {
	mn := &v1alpha1.MeshNetworks{
		Networks: make(map[string]*v1alpha1.Network),
	}

	if current.DisableRegistryJoin {
		return mn, nil
	}

	for context, cluster := range mesh.clustersByContext {
		if _, ok := env.GetConfig().Contexts[context]; !ok {
			return nil, fmt.Errorf("context %v not found", context)
		}

		// Don't include this cluster in the mesh's network ye t.
		if cluster.DisableRegistryJoin {
			continue
		}

		network := cluster.Network
		if _, ok := mn.Networks[network]; !ok {
			mn.Networks[network] = &v1alpha1.Network{}
		}

		for _, gateway := range cluster.readIngressGateways() {
			// TODO debug why RegistryServiceName doesn't work
			mn.Networks[network].Gateways = append(mn.Networks[network].Gateways,
				&v1alpha1.Network_IstioNetworkGateway{
					Gw: &v1alpha1.Network_IstioNetworkGateway_Address{
						gateway.Address,
					},
					Port:     gateway.Port,
					Locality: gateway.Locality,
				},
			)

		}

		// Use the cluster clusterName for the registry name so we have consistency across the mesh. Pilot
		// uses a special name for the local cluster against which it is running.
		registry := cluster.clusterName
		if context == current.Context {
			registry = string(serviceregistry.Kubernetes)
		}

		mn.Networks[network].Endpoints = append(mn.Networks[network].Endpoints,
			&v1alpha1.Network_NetworkEndpoints{
				Ne: &v1alpha1.Network_NetworkEndpoints_FromRegistry{
					FromRegistry: registry,
				},
			},
		)
	}

	return mn, nil
}

type generateOptions struct {
	KubeOptions
	filenameOption
	from            string
	waitForGateways bool
}

func (o *generateOptions) addFlags(flagset *pflag.FlagSet) {
	o.filenameOption.addFlags(flagset)

	flagset.StringVar(&o.from, "from", "",
		"optional source configuration to generate multicluster aware configuration from")
	flagset.BoolVar(&o.waitForGateways, "wait-for-gateways", false,
		"wait for all cluster's istio-ingressgateway IPs to be ready before generating configuration.")
}

func (o *generateOptions) prepare(flags *pflag.FlagSet) error {
	o.KubeOptions.prepare(flags)
	return o.filenameOption.prepare()
}

func NewGenerateCommand() *cobra.Command {
	opt := generateOptions{}
	c := &cobra.Command{
		Use:   "generate -f <mesh.yaml>",
		Short: `generate a cluster-specific control plane configuration based on the mesh description and runtime state`,
		RunE: func(c *cobra.Command, args []string) error {
			if err := opt.prepare(c.Flags()); err != nil {
				return err
			}
			env, err := NewEnvironmentFromCobra(opt.Kubeconfig, opt.Context, c)
			if err != nil {
				return err
			}
			return generateOutput(opt, env)
		},
	}
	opt.addFlags(c.PersistentFlags())
	return c
}
