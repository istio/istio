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

	v1 "k8s.io/api/core/v1"

	"github.com/hashicorp/go-version"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/util"
	"istio.io/pkg/log"
)

// ExecClient is an interface for remote execution
// TODO: replace with correct type.
type ExecClient interface {
	PodsForSelector(namespace, labelSelector string) (*v1.PodList, error)
}

// hook is a callout function that may be called during an upgrade to check state or modify the cluster.
// hooks should only be used for version-specific actions.
type hook func(kubeClient ExecClient, sourceValues, targetValues *v1alpha2.IstioControlPlaneSpec) util.Errors
type hooks []hook

// hookVersionMapping is a mapping between a hashicorp/go-version formatted constraints for the source and target
// versions and the list of hooks that should be run if the constraints match.
type hookVersionMapping struct {
	sourceVersionConstraint string
	targetVersionConstraint string
	hooks                   hooks
}

// hookCommonParams is a set of common params passed to all hooks.
type hookCommonParams struct {
	sourceVer    string
	targetVer    string
	sourceValues *v1alpha2.IstioControlPlaneSpec
	targetValues *v1alpha2.IstioControlPlaneSpec
}

var (
	// preUpgradeHooks is a list of hook version constraint pairs mapping to a slide of corresponding hooks to run
	// before upgrade.
	preUpgradeHooks = []hookVersionMapping{
		{
			sourceVersionConstraint: ">=1.3",
			targetVersionConstraint: ">=1.3",
			hooks:                   []hook{checkInitCrdJobs},
		},
	}
	// postUpgradeHooks is a list of hook version constraint pairs mapping to a slide of corresponding hooks to run
	// before upgrade.
	postUpgradeHooks []hookVersionMapping
)

func runPreUpgradeHooks(kubeClient ExecClient, hc *hookCommonParams, dryRun bool) util.Errors {
	return runUpgradeHooks(preUpgradeHooks, kubeClient, hc, dryRun)
}

func runPostUpgradeHooks(kubeClient ExecClient, hc *hookCommonParams, dryRun bool) util.Errors {
	return runUpgradeHooks(postUpgradeHooks, kubeClient, hc, dryRun)
}

// runUpgradeHooks checks a list of hook version map entries and runs the hooks in each entry whose constraints match
// the source/target versions in hc.
func runUpgradeHooks(hml []hookVersionMapping, kubeClient ExecClient, hc *hookCommonParams, dryRun bool) util.Errors {
	var errs util.Errors
	_, err := version.NewVersion(hc.sourceVer)
	if err != nil {
		return util.NewErrs(err)
	}
	_, err = version.NewVersion(hc.targetVer)
	if err != nil {
		return util.NewErrs(err)
	}

	for _, h := range hml {
		matches, err := checkHookListEntry(h, hc)
		if err != nil {
			errs = util.AppendErr(errs, err)
			continue
		}
		if !matches {
			continue
		}
		log.Infof("Running the following hooks which match source->target versions %s->%s: %s", hc.sourceVer, hc.targetVer, h.hooks)
		if dryRun {
			log.Info("(Skipping running hooks due to dry-run being set.)")
			continue
		}
		for _, hf := range h.hooks {
			log.Infof("Running hook %s", hf)
			errs = util.AppendErrs(errs, hf(kubeClient, hc.sourceValues, hc.targetValues))
		}
	}
	return errs
}

// checkHookListEntry checks a hookVersionMapping against the source/target versions in hc and returns true if it
// matches.
func checkHookListEntry(h hookVersionMapping, hc *hookCommonParams) (bool, error) {
	ch, err := checkConstraint(hc.sourceVer, h.sourceVersionConstraint)
	if err != nil {
		return false, err
	}
	if !ch {
		log.Infof("Source version %s does not satisfy source constraint %s, skip hooks", hc.sourceVer, h.sourceVersionConstraint)
		return false, nil
	}

	ch, err = checkConstraint(hc.targetVer, h.targetVersionConstraint)
	if err != nil {
		return false, err
	}
	if !ch {
		log.Infof("Target version %s does not satisfy target constraint %s, skip hooks", hc.targetVer, h.targetVersionConstraint)
		return false, nil
	}
	return true, nil
}

// checkConstraint reports whether SemVer formatted string verStr matches hashicorp/go-version formatted constraints
// in constraintStr.
func checkConstraint(verStr, constraintStr string) (bool, error) {
	ver, err := version.NewVersion(verStr)
	if err != nil {
		return false, err
	}
	constraint, err := version.NewConstraint(constraintStr)
	if err != nil {
		return false, err
	}
	return constraint.Check(ver), nil
}

func checkInitCrdJobs(kubeClient ExecClient, currentValues, _ *v1alpha2.IstioControlPlaneSpec) util.Errors {
	pl, err := kubeClient.PodsForSelector(currentValues.DefaultNamespace, "")
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
