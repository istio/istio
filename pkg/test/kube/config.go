//  Copyright 2018 Istio Authors
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
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	kubelib "istio.io/istio/pkg/kube"
)

// CreateConfig returns rest config based in the given path. It *only* loads from the provided path,
// and avoids falling back to other known places.
func CreateConfig(configPath string) (*rest.Config, error) {
	// Only use the provided configPath and *do not* fallback to anything else.
	loadingRules := &clientcmd.ClientConfigLoadingRules{}
	loadingRules.ExplicitPath = configPath
	overrides := &clientcmd.ConfigOverrides{}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
}

// newRestClient creates a rest client with default config.
func newRestClient(kubeconfig, configContext string) (*rest.RESTClient, *rest.Config, error) {
	config, err := defaultRestConfig(kubeconfig, configContext)
	if err != nil {
		return nil, nil, err
	}
	restClient, err := rest.RESTClientFor(config)
	if err != nil {
		return nil, nil, err
	}
	return restClient, config, nil
}

func defaultRestConfig(kubeconfig, configContext string) (*rest.Config, error) {
	config, err := kubelib.BuildClientConfig(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}
	config.APIPath = "/api"
	config.GroupVersion = &v1.SchemeGroupVersion
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: scheme.Codecs}
	return config, nil
}
