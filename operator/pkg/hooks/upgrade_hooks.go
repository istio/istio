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

package hooks

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/hashicorp/go-version"

	"istio.io/api/operator/v1alpha1"
	iop "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/util"
)

const (
	configAPIGroup   = "config.istio.io"
	configAPIVersion = "v1alpha2"
	istioNamespace   = "istio-system"
)

var (
	// preUpgradeHooks is a list of hook version constraint pairs mapping to a slide of corresponding hooks to run
	// before upgrade.
	preUpgradeHooks = []hookVersionMapping{
		{
			sourceVersionConstraint: ">=1.3",
			targetVersionConstraint: ">=1.3",
			hooks:                   []hook{checkInitCrdJobs, checkMixerTelemetry},
		},
	}
	// postUpgradeHooks is a list of hook version constraint pairs mapping to a slide of corresponding hooks to run
	// before upgrade.
	postUpgradeHooks []hookVersionMapping
)

func RunPreUpgradeHooks(kubeClient manifest.ExecClient, hc *HookCommonParams, dryRun bool) util.Errors {
	return runUpgradeHooks(preUpgradeHooks, kubeClient, hc, dryRun)
}

func RunPostUpgradeHooks(kubeClient manifest.ExecClient, hc *HookCommonParams, dryRun bool) util.Errors {
	return runUpgradeHooks(postUpgradeHooks, kubeClient, hc, dryRun)
}

func checkInitCrdJobs(kubeClient manifest.ExecClient, params HookCommonParams) util.Errors {
	pl, err := kubeClient.PodsForSelector(params.SourceIOPS.MeshConfig.RootNamespace, "")
	if err != nil {
		return util.NewErrs(fmt.Errorf("failed to list pods: %v", err))
	}

	for _, p := range pl.Items {
		if strings.Contains(p.Name, "istio-init-crd") {
			return util.NewErrs(fmt.Errorf("istio-init-crd pods exist: %v. Istio was installed with non-operator methods, "+
				"please migrate to operator installation first", p.Name))
		}
	}

	return nil
}

func (h hooks) String() string {
	var out []string
	for _, hh := range h {
		out = append(out, hh.String())
	}
	return strings.Join(out, ", ")
}

func (h hook) String() string {
	return getFunctionName(h)
}

func getFunctionName(i interface{}) string {
	return runtime.FuncForPC(reflect.ValueOf(i).Pointer()).Name()
}
