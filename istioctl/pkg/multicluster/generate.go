// Copyright 2019 Istio Authors.
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
	"bytes"
	"fmt"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/client-go/tools/clientcmd/api"

	"istio.io/api/mesh/v1alpha1"

	"istio.io/istio/pkg/util/protomarshal"
)

// TODO replace with common strongly typed values.yaml when available
type valueType struct {
	Global struct {
		MeshNetworks                map[string]interface{} `json:"meshNetworks,omitempty"`
		MeshID                      string                 `json:"meshID,omitempty"`
		Network                     string                 `json:"network,omitempty"`
		ControlPlaneSecurityEnabled bool                   `json:"controlPlaneSecurityEnabled,omitempty"`
		MultiCluster                struct {
			ClusterName string `json:"clusterName,omitempty"`
		} `json:"multiCluster,omitempty"`
		MTLS struct {
			Enabled bool `json:"enabled,omitempty"`
		} `json:"mtls,omitempty"`
	} `json:"global,omitempty"`

	Gateways struct {
		IstioIngressGateway struct {
			Env map[string]string `json:"env,omitempty"`
		} `json:"istio-ingressgateway,omitempty"`
	} `json:"gateways,omitempty"`

	Security struct {
		SelfSigned bool `json:"selfSigned,omitempty"`
	}
}

func generateValuesYAML(mesh *Mesh, current *KubeCluster, meshNetworks *v1alpha1.MeshNetworks) (string, error) {
	meshNetworksJSON, err := protomarshal.ToJSONMap(meshNetworks)
	if err != nil {
		return "", err
	}

	var values valueType
	values.Global.MeshNetworks = meshNetworksJSON
	values.Global.MeshID = mesh.meshID
	values.Global.Network = current.Network
	values.Global.ControlPlaneSecurityEnabled = true
	values.Global.MTLS.Enabled = true
	values.Global.MultiCluster.ClusterName = current.UniqueName()
	values.Security.SelfSigned = mesh.hints.SelfSigned
	values.Gateways.IstioIngressGateway.Env["ISTIO_MESH_NETWORK"] = current.Network // Istio <= 1.3

	//values := map[string]interface{}{
	//	"global": map[string]interface{}{
	//		"meshNetworks":                meshNetworksJSON["networks"],
	//		"meshID":                      mesh.meshID,
	//		"network":                     current.Network,
	//		"controlPlaneSecurityEnabled": true,
	//		"multiCluster": map[string]interface{}{
	//			"clusterName": current.UniqueName(),
	//		},
	//		"mtls": map[string]interface{}{
	//			"enabled": true,
	//		},
	//	},
	//
	//	// TODO required for Istio <= 1.3.x
	//	"gateways": map[string]interface{}{
	//		"istio-ingressgateway": map[string]interface{}{
	//			"env": map[string]interface{}{
	//				"ISTIO_META_NETWORK": current.Network,
	//			},
	//		},
	//	},
	//
	//	"security": map[string]interface{}{
	//		"selfSigned": mesh.hints.SelfSigned,
	//	},
	//}

	valuesStr, err := yaml.Marshal(values)
	if err != nil {
		return "", err
	}

	header := fmt.Sprintf("# auto-generated values.yaml for cluster %q\n", current.context)
	var buf bytes.Buffer
	if _, err := buf.WriteString(header); err != nil {
		return "", err
	}
	if _, err := buf.Write(valuesStr); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func generateValues(opt generateOptions, env Environment) error {
	mesh, err := meshFromFileDesc(opt.filename, opt.Kubeconfig, env)
	if err != nil {
		return err
	}

	context := opt.Context
	if context == "" {
		context = env.GetConfig().CurrentContext
	}

	cluster, ok := mesh.clusters[context]
	if !ok {
		return fmt.Errorf("Context %v not found", context)
	}

	meshNetwork, err := meshNetworkForCluster(env.GetConfig(), mesh, cluster)
	if err != nil {
		return err
	}

	out, err := generateValuesYAML(mesh, cluster, meshNetwork)
	if err != nil {
		return err
	}

	fmt.Fprintf(env.Stdout(), out)

	return nil
}

func meshNetworkForCluster(apiConfig *api.Config, mesh *Mesh, current *KubeCluster) (*v1alpha1.MeshNetworks, error) {
	mn := &v1alpha1.MeshNetworks{
		Networks: map[string]*v1alpha1.Network{},
	}

	for context, cluster := range mesh.clusters {
		if _, ok := apiConfig.Contexts[context]; !ok {
			return nil, fmt.Errorf("Context %v not found", context)
		}

		// Don't include this cluster in the mesh's network yet.
		if cluster.DisableServiceDiscovery {
			continue
		}

		network := cluster.Network
		if _, ok := mn.Networks[network]; !ok {
			mn.Networks[network] = &v1alpha1.Network{}
		}

		// TODO debug why RegistryServiceName doesn't work
		for _, address := range current.state.gatewayAddresses {
			mn.Networks[network].Gateways = append(mn.Networks[network].Gateways,
				&v1alpha1.Network_IstioNetworkGateway{
					Gw: &v1alpha1.Network_IstioNetworkGateway_Address{
						address,
					},
					Port: 443,
				},
			)
		}

		// Use the unique cluster name for the registry so we have consistency across the mesh. Pilot
		// uses a special name for the local cluster against which it is running.
		registry := current.UniqueName()
		if context == current.context {
			registry = selfRegistryName
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
}

func (o *generateOptions) addFlags(flagset *pflag.FlagSet) {
	o.filenameOption.addFlags(flagset)
}

func (o *generateOptions) prepare(flags *pflag.FlagSet) error {
	o.KubeOptions.prepare(flags)
	return o.filenameOption.prepare()
}

func NewGenerateCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "generate",
		Short: `generate configuration for setting up a multi-cluster mesh`,
	}

	// TODO(ayj) NewGenerateTrustAnchor(), NewGenerateRemoteSecret()
	c.AddCommand(
		NewGenerateValuesCommand(),
	)
	return c
}

func NewGenerateValuesCommand() *cobra.Command {
	opt := generateOptions{}
	c := &cobra.Command{
		Use: "remote-secret",
		RunE: func(c *cobra.Command, args []string) error {
			if err := opt.prepare(c.Flags()); err != nil {
				return err
			}
			env, err := newKubeEnvFromCobra(opt.Kubeconfig, opt.Context, c)
			if err != nil {
				return err
			}
			return generateValues(opt, env)
		},
	}
	opt.addFlags(c.PersistentFlags())
	return c
}
