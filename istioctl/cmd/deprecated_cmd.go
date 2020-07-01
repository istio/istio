// Copyright Istio Authors
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

// DEPRECATED - These commands are deprecated and will be removed in future releases.

package cmd

import (
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/model"
	controller2 "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	kubecfg "istio.io/istio/pkg/kube"
)

var (
	// Create a model.ConfigStore (or sortedConfigStore)
	configStoreFactory = newConfigStore
)

func newConfigStore() (model.ConfigStore, error) {
	cfg, err := kubecfg.BuildClientConfig(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}

	client, err := crdclient.NewForConfig(cfg, &model.DisabledLedger{}, "", controller2.Options{DomainSuffix: ""})
	if err != nil {
		return nil, err
	}

	stop := make(chan struct{})
	// TODO: replace with direct istio/client-go usage so we don't have to do this part
	go client.Run(stop)
	cache.WaitForCacheSync(stop, client.HasSynced)

	return client, nil
}
