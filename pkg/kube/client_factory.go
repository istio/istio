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
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/openapi"
	"k8s.io/kubectl/pkg/validation"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

var _ util.Factory = &clientFactory{}

// clientFactory implements the kubectl util.Factory, which is provides access to various k8s clients.
type clientFactory struct {
	clientConfig clientcmd.ClientConfig
	factory      util.Factory

	mapperOnce sync.Once
	mapper     meta.RESTMapper
	expander   meta.RESTMapper

	discoveryOnce   sync.Once
	discoveryClient discovery.CachedDiscoveryInterface
}

// newClientFactory creates a new util.Factory from the given clientcmd.ClientConfig.
func newClientFactory(clientConfig clientcmd.ClientConfig) util.Factory {
	out := &clientFactory{
		clientConfig: clientConfig,
	}

	out.factory = util.NewFactory(out)
	return out
}

func (c *clientFactory) ToRESTConfig() (*rest.Config, error) {
	restConfig, err := c.clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	return SetRestDefaults(restConfig), nil
}

func (c *clientFactory) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	c.discoveryOnce.Do(func() {
		restConfig, err := c.ToRESTConfig()
		if err != nil {
			return
		}
		d, err := discovery.NewDiscoveryClientForConfig(restConfig)
		if err != nil {
			return
		}
		c.discoveryClient = memory.NewMemCacheClient(d)
	})
	return c.discoveryClient, nil
}

func (c *clientFactory) ToRESTMapper() (meta.RESTMapper, error) {
	discoveryClient, err := c.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	rc, err := c.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	c.mapperOnce.Do(func() {
		c.mapper, _ = apiutil.NewDynamicRESTMapper(rc, apiutil.WithLazyDiscovery, apiutil.WithCustomMapper(func() (meta.RESTMapper, error) {
			discoveryClient.Invalidate()
			// Use a custom mapper so we can re-use our discoveryClient instead of creating a new one
			groupResources, err := restmapper.GetAPIGroupResources(discoveryClient)
			if err != nil {
				return nil, err
			}
			return restmapper.NewDiscoveryRESTMapper(groupResources), nil
		}))
		c.expander = restmapper.NewShortcutExpander(c.mapper, discoveryClient)
	})
	return c.expander, nil
}

func (c *clientFactory) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return c.clientConfig
}

func (c *clientFactory) DynamicClient() (dynamic.Interface, error) {
	restConfig, err := c.ToRESTConfig()
	if err != nil {
		return nil, err
	}

	return dynamic.NewForConfig(restConfig)
}

func (c *clientFactory) KubernetesClientSet() (*kubernetes.Clientset, error) {
	restConfig, err := c.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(restConfig)
}

func (c *clientFactory) RESTClient() (*rest.RESTClient, error) {
	return c.factory.RESTClient()
}

func (c *clientFactory) NewBuilder() *resource.Builder {
	return c.factory.NewBuilder()
}

func (c *clientFactory) ClientForMapping(mapping *meta.RESTMapping) (resource.RESTClient, error) {
	return c.factory.ClientForMapping(mapping)
}

func (c *clientFactory) UnstructuredClientForMapping(mapping *meta.RESTMapping) (resource.RESTClient, error) {
	return c.factory.UnstructuredClientForMapping(mapping)
}

func (c *clientFactory) Validator(validate bool) (validation.Schema, error) {
	return c.factory.Validator(validate)
}

func (c *clientFactory) OpenAPISchema() (openapi.Resources, error) {
	return c.factory.OpenAPISchema()
}

func (c *clientFactory) OpenAPIGetter() discovery.OpenAPISchemaInterface {
	return c.factory.OpenAPIGetter()
}
