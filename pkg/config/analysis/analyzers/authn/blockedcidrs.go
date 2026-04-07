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

package authn

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
)

// BlockedCIDRsAnalyzer checks that BLOCKED_CIDRS_IN_JWKS_URIS is configured on istiod
// when RequestAuthentication resources exist.
type BlockedCIDRsAnalyzer struct{}

var _ analysis.Analyzer = &BlockedCIDRsAnalyzer{}

func (a *BlockedCIDRsAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "authn.BlockedCIDRsAnalyzer",
		Description: "Checks that BLOCKED_CIDRS_IN_JWKS_URIS is configured on istiod when RequestAuthentication resources exist",
		Inputs: []config.GroupVersionKind{
			gvk.RequestAuthentication,
			gvk.Deployment,
		},
	}
}

func (a *BlockedCIDRsAnalyzer) Analyze(c analysis.Context) {
	// Collect all RequestAuthentication resources.
	var raNames []string
	c.ForEach(gvk.RequestAuthentication, func(r *resource.Instance) bool {
		raNames = append(raNames, r.Metadata.FullName.String())
		return true
	})
	if len(raNames) == 0 {
		return
	}

	// Find istiod deployments and check for the env var.
	c.ForEach(gvk.Deployment, func(r *resource.Instance) bool {
		if !isIstiodDeployment(r) {
			return true
		}
		d := r.Message.(*appsv1.DeploymentSpec)
		if hasBlockedCIDRsEnv(d.Template.Spec.Containers) {
			return true
		}

		names := formatRANames(raNames)
		m := msg.NewJwksUriFetchUnrestricted(r, len(raNames), names)
		c.Report(gvk.Deployment, m)
		return true
	})
}

func isIstiodDeployment(r *resource.Instance) bool {
	return r.Metadata.Labels["app"] == "istiod"
}

func hasBlockedCIDRsEnv(containers []v1.Container) bool {
	for _, c := range containers {
		for _, e := range c.Env {
			if e.Name == "BLOCKED_CIDRS_IN_JWKS_URIS" && e.Value != "" {
				return true
			}
		}
	}
	return false
}

func formatRANames(names []string) string {
	const maxDisplay = 5
	if len(names) <= maxDisplay {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(names[:maxDisplay], ", "), len(names)-maxDisplay)
}
