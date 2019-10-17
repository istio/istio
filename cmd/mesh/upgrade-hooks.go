// Copyright 2019 Istio Authors
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

package mesh

import (
	"strings"

	"istio.io/operator/pkg/manifest"
)

type hook func(kubeClient manifest.ExecClient, istioNamespace,
	currentVer, targetVer, currentValues, targetValues string, dryRun bool, l *logger)

func runPreUpgradeHooks(kubeClient manifest.ExecClient, istioNamespace,
	currentVer, targetVer, currentValues, targetValues string, dryRun bool, l *logger) {
	for _, h := range preUpgradeHooks {
		h(kubeClient, istioNamespace, currentVer, targetVer,
			currentValues, targetValues, dryRun, l)
	}
}

func runPostUpgradeHooks(kubeClient manifest.ExecClient, istioNamespace,
	currentVer, targetVer, currentValues, targetValues string, dryRun bool, l *logger) {
	for _, h := range prePostUpgradeHooks {
		h(kubeClient, istioNamespace, currentVer, targetVer,
			currentValues, targetValues, dryRun, l)
	}
}

var preUpgradeHooks = []hook{checkInitCrdJobs}
var prePostUpgradeHooks = []hook{}

func checkInitCrdJobs(kubeClient manifest.ExecClient, istioNamespace,
	currentVer, targetVer, currentValues, targetValues string, dryRun bool, l *logger) {
	pl, err := kubeClient.PodsForSelector(istioNamespace, "")
	if err != nil {
		l.logAndFatalf("Abort. Failed to list pods: %v", err)
	}

	for _, p := range pl.Items {
		if strings.Contains(p.Name, "istio-init-crd") {
			l.logAndFatalf("Abort. istio-init-crd pods exist: %v. "+
				"Istio was installed with non-operator methods, "+
				"please migrate to operator installation first.", p.Name)
		}
	}
}
