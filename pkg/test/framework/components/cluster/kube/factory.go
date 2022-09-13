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
	"net/http"
	"net/url"

	"k8s.io/client-go/rest"

	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/reserveport"
)

const (
	kubeconfigMetaKey = "kubeconfig"
	vmSupportMetaKey  = "fakeVM"
)

func init() {
	cluster.RegisterFactory(cluster.Kubernetes, buildKube)
}

func buildKube(origCfg cluster.Config, topology cluster.Topology) (cluster.Cluster, error) {
	cfg, err := validConfig(origCfg)
	if err != nil {
		return nil, err
	}

	kubeconfigPath := cfg.Meta.String(kubeconfigMetaKey)
	kubeconfigPath, err = file.NormalizePath(kubeconfigPath)
	if err != nil {
		return nil, err
	}

	var client istioKube.CLIClient
	if len(cfg.HTTPProxy) > 0 {
		proxyURL, err := url.Parse(cfg.HTTPProxy)
		if err != nil {
			return nil, err
		}
		client, err = buildClientWithProxy(kubeconfigPath, proxyURL)
		if err != nil {
			return nil, err
		}
	} else {
		client, err = buildClient(kubeconfigPath)
		if err != nil {
			return nil, err
		}
	}
	m, err := reserveport.NewPortManager()
	if err != nil {
		return nil, err
	}
	client.SetPortManager(m.ReservePortNumber)

	// support fake VMs by default
	vmSupport := true
	if vmP := cfg.Meta.Bool(vmSupportMetaKey); vmP != nil {
		vmSupport = *vmP
	}

	return &Cluster{
		filename:  kubeconfigPath,
		CLIClient: client,
		vmSupport: vmSupport,
		Topology:  topology,
	}, nil
}

func validConfig(cfg cluster.Config) (cluster.Config, error) {
	// only include kube-specific validation here
	if cfg.Meta.String(kubeconfigMetaKey) == "" {
		return cfg, fmt.Errorf("missing meta.%s for %s", kubeconfigMetaKey, cfg.Name)
	}
	return cfg, nil
}

func buildClient(kubeconfig string) (istioKube.CLIClient, error) {
	rc, err := istioKube.DefaultRestConfig(kubeconfig, "", func(config *rest.Config) {
		config.QPS = 200
		config.Burst = 400
	})
	if err != nil {
		return nil, err
	}
	return istioKube.NewCLIClient(istioKube.NewClientConfigForRestConfig(rc), "")
}

func buildClientWithProxy(kubeconfig string, proxyURL *url.URL) (istioKube.CLIClient, error) {
	rc, err := istioKube.DefaultRestConfig(kubeconfig, "", func(config *rest.Config) {
		config.QPS = 200
		config.Burst = 400
	})
	if err != nil {
		return nil, err
	}
	rc.Proxy = http.ProxyURL(proxyURL)
	return istioKube.NewCLIClient(istioKube.NewClientConfigForRestConfig(rc), "")
}
