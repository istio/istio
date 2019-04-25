// Copyright 2018 The Operator-SDK Authors
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

package client

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/helm/pkg/kube"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// NewFromManager returns a Kubernetes client that can be used with
// a Tiller server.
func NewFromManager(mgr manager.Manager) (*kube.Client, error) {
	c, err := newClientGetter(mgr)
	if err != nil {
		return nil, err
	}
	return kube.New(c), nil
}

type clientGetter struct {
	restConfig      *rest.Config
	discoveryClient discovery.CachedDiscoveryInterface
	restMapper      meta.RESTMapper
}

func (c *clientGetter) ToRESTConfig() (*rest.Config, error) {
	return c.restConfig, nil
}

func (c *clientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	return c.discoveryClient, nil
}

func (c *clientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	return c.restMapper, nil
}

func (c *clientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return nil
}

func newClientGetter(mgr manager.Manager) (*clientGetter, error) {
	cfg := mgr.GetConfig()
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, err
	}
	cdc := cached.NewMemCacheClient(dc)
	rm := mgr.GetRESTMapper()

	return &clientGetter{
		restConfig:      cfg,
		discoveryClient: cdc,
		restMapper:      rm,
	}, nil
}
