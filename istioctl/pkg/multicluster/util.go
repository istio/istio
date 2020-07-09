// Copyright Istio Authors.
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

package multicluster

import (
	"fmt"

	"istio.io/istio/pkg/kube"
)

func contextOrDefault(kubeconfig, context string) (string, error) {
	config := kube.BuildClientCmd(kubeconfig, context)
	rawCfg, err := config.RawConfig()
	if err != nil {
		return "", err
	}

	if context == "" {
		return rawCfg.CurrentContext, nil
	}

	// Make sure the provided context exists in the kubeconfig.
	if _, ok := rawCfg.Contexts[context]; ok {
		return context, nil
	}

	return "", fmt.Errorf("context %s missing in %s", context, kubeconfig)
}
