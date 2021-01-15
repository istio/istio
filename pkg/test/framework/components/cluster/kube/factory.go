//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package kube

import (
	"fmt"

	"github.com/hashicorp/go-multierror"
	"k8s.io/client-go/rest"

	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
)

// NewFactory creates a new kube Cluster factory, using a slice-pointer that will be filled
// with every possibly-related cluster, to allow checking topology info (network, primary, config).
func NewFactory(allClusters map[string]resource.Cluster) cluster.Factory {
	return &factory{allClusters: allClusters}
}

type factory struct {
	allClusters map[string]resource.Cluster
	configs     []cluster.Config
}

func (f *factory) Kind() cluster.Kind {
	return cluster.Kubernetes
}

func (f *factory) With(configs ...cluster.Config) cluster.Factory {
	for _, c := range configs {
		f.configs = append(f.configs, c)
	}
	return f
}

func (f *factory) Build() (resource.Clusters, error) {
	var errs error

	var clusters resource.Clusters
	for _, origCfg := range f.configs {
		cfg, err := validConfig(origCfg)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}
		kubeconfigPath := cfg.Meta["kubeconfig"]
		client, err := buildClient(kubeconfigPath)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}
		clusters = append(clusters, &Cluster{
			filename:       kubeconfigPath,
			ExtendedClient: client,
			Topology: cluster.NewTopology(
				cfg.Name,
				cfg.Network,
				cfg.ControlPlaneClusterName,
				cfg.ConfigClusterName,
				f.allClusters,
			),
		})
	}

	return clusters, nil
}

func validConfig(cfg cluster.Config) (cluster.Config, error) {
	// only include kube-specific validation here
	if cfg.Meta == nil || cfg.Meta["kubeconfig"] == "" {
		return cfg, fmt.Errorf("missing meta.kubeconfig for %s", cfg.Name)
	}
	return cfg, nil
}

func buildClient(kubeconfig string) (istioKube.ExtendedClient, error) {
	rc, err := istioKube.DefaultRestConfig(kubeconfig, "", func(config *rest.Config) {
		config.QPS = 200
		config.Burst = 400
	})
	if err != nil {
		return nil, err
	}
	return istioKube.NewExtendedClient(istioKube.NewClientConfigForRestConfig(rc), "")
}
