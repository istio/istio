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
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/discovery"
	diskcached "k8s.io/client-go/discovery/cached/disk"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/openapi"
	"k8s.io/kubectl/pkg/validation"
)

var (
	defaultCacheDir = filepath.Join(homedir.HomeDir(), ".kube", "http-cache")
)

var _ util.Factory = &clientFactory{}

// clientFactory implements the kubectl util.Factory, which is provides access to various k8s clients.
type clientFactory struct {
	clientConfig clientcmd.ClientConfig
	cacheDir     string
	factory      util.Factory
}

// NewClientFactory creates a new util.Factory instance based on the config.
func NewClientFactory(clientConfig clientcmd.ClientConfig, cacheDir string) util.Factory {
	out := &clientFactory{
		clientConfig: clientConfig,
		cacheDir:     cacheDir,
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
	restConfig, err := c.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	return toDiscoveryClient(restConfig, c.cacheDir)
}

func (c *clientFactory) ToRESTMapper() (meta.RESTMapper, error) {
	discoveryClient, err := c.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient)
	expander := restmapper.NewShortcutExpander(mapper, discoveryClient)
	return expander, nil
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

func toDiscoveryClient(config *rest.Config, cacheDir string) (discovery.CachedDiscoveryInterface, error) {
	// The more groups you have, the more discovery requests you need to make.
	// given 25 groups (our groups + a few custom resources) with one-ish version each, discovery needs to make 50 requests
	// double it just so we don't end up here again for a while.  This config is only used for discovery.
	config.Burst = 100

	// retrieve a user-provided value for the "cache-dir"
	// defaulting to ~/.kube/http-cache if no user-value is given.
	httpCacheDir := defaultCacheDir
	if len(cacheDir) > 0 {
		httpCacheDir = cacheDir
	}

	discoveryCacheDir := computeDiscoverCacheDir(filepath.Join(homedir.HomeDir(), ".kube", "cache", "discovery"), config.Host)
	return diskcached.NewCachedDiscoveryClientForConfig(config, discoveryCacheDir, httpCacheDir, 10*time.Minute)
}

// overlyCautiousIllegalFileCharacters matches characters that *might* not be supported.  Windows is really restrictive, so this is really restrictive
var overlyCautiousIllegalFileCharacters = regexp.MustCompile(`[^(\w/.)]`)

// computeDiscoverCacheDir takes the parentDir and the host and comes up with a "usually non-colliding" name.
func computeDiscoverCacheDir(parentDir, host string) string {
	// strip the optional scheme from host if its there:
	schemelessHost := strings.Replace(strings.Replace(host, "https://", "", 1), "http://", "", 1)
	// now do a simple collapse of non-AZ09 characters.  Collisions are possible but unlikely.  Even if we do collide the problem is short lived
	safeHost := overlyCautiousIllegalFileCharacters.ReplaceAllString(schemelessHost, "_")
	return filepath.Join(parentDir, safeHost)
}
