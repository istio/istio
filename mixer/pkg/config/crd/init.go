// Copyright 2017 Istio Authors
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

package crd

import (
	"net/url"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	// import GKE cluster authentication plugin
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	// import OIDC cluster authentication plugin, e.g. for Tectonic
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"

	"istio.io/mixer/pkg/config/store"
)

// defaultDiscoveryBuilder builds the actual discovery client using the kubernetes config.
func defaultDiscoveryBuilder(conf *rest.Config) (discovery.DiscoveryInterface, error) {
	client, err := discovery.NewDiscoveryClientForConfig(conf)
	return client, err
}

// dynamicListenerWatcherBuilder is the builder of cache.ListerWatcher by using actual
// k8s.io/client-go/dynamic.Client.
type dynamicListerWatcherBuilder struct {
	client *dynamic.Client
}

func newDynamicListenerWatcherBuilder(conf *rest.Config) (listerWatcherBuilderInterface, error) {
	client, err := dynamic.NewClient(conf)
	if err != nil {
		return nil, err
	}
	return &dynamicListerWatcherBuilder{client}, nil
}

func (b *dynamicListerWatcherBuilder) build(res metav1.APIResource) cache.ListerWatcher {
	return b.client.Resource(&res, "")
}

// NewStore creates a new Store instance.
func NewStore(u *url.URL) (store.Store2Backend, error) {
	kubeconfig := u.Path
	namespaces := u.Query().Get("ns")
	conf, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	conf.APIPath = "/apis"
	conf.GroupVersion = &schema.GroupVersion{Group: apiGroup, Version: apiVersion}
	s := &Store{
		conf:                 conf,
		discoveryBuilder:     defaultDiscoveryBuilder,
		listerWatcherBuilder: newDynamicListenerWatcherBuilder,
	}
	if len(namespaces) > 0 {
		s.ns = map[string]bool{}
		for _, n := range strings.Split(namespaces, ",") {
			s.ns[n] = true
		}
	}
	return s, nil
}

// Register registers this module as a Store2Backend.
// Do not use 'init()' for automatic registration; linker will drop
// the whole module because it looks unused.
func Register(builders map[string]store.Store2Builder) {
	builders["k8s"] = NewStore
	builders["kube"] = NewStore
	builders["kubernetes"] = NewStore
}
