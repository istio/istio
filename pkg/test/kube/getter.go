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
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

var _ resource.RESTClientGetter = &clientGetter{}

type clientGetter struct {
	restConfig *rest.Config
}

func newClientGetter(restConfig *rest.Config) resource.RESTClientGetter {
	return &clientGetter{
		restConfig: restConfig,
	}
}

func (c *clientGetter) ToRESTConfig() (*rest.Config, error) {
	return copyRestConfig(c.restConfig), nil
}

func (c *clientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	d, err := discovery.NewDiscoveryClientForConfig(copyRestConfig(c.restConfig))
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(d), nil
}

func (c *clientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	discoveryClient, err := c.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient)
	expander := restmapper.NewShortcutExpander(mapper, discoveryClient)
	return expander, nil
}
