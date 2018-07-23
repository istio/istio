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
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
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
