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

package k8sgateway

import (
	"github.com/Masterminds/semver/v3"
	"sigs.k8s.io/gateway-api/pkg/consts"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gatewayapi"
	"istio.io/istio/pkg/config/schema/gvk"
)

// CRDVersionAnalyzer flags Gateway API CRDs whose installed bundle version is
// below the minimum required by this Istio binary. When CRDs are too old, the
// CRD watcher silently filters them out so the matching resources are never
// processed (e.g. TLSRoute v1.4 with Istio 1.30 causes TLS passthrough Gateways
// to report attachedRoutes: 0 and never program a listener).
type CRDVersionAnalyzer struct{}

var _ analysis.Analyzer = &CRDVersionAnalyzer{}

func (*CRDVersionAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "k8sgateway.CRDVersionAnalyzer",
		Description: "Checks Gateway API CRDs are at the minimum version required by this Istio version",
		Inputs: []config.GroupVersionKind{
			gvk.CustomResourceDefinition,
		},
	}
}

func (a *CRDVersionAnalyzer) Analyze(ctx analysis.Context) {
	ctx.ForEach(gvk.CustomResourceDefinition, func(r *resource.Instance) bool {
		a.analyzeCRD(r, ctx)
		return true
	})
}

func (*CRDVersionAnalyzer) analyzeCRD(r *resource.Instance, ctx analysis.Context) {
	name := r.Metadata.FullName.Name.String()
	minimum, ok := gatewayapi.MinimumCRDVersions[name]
	if !ok {
		return
	}
	bv, ok := r.Metadata.Annotations[consts.BundleVersionAnnotation]
	if !ok {
		// Watcher will surface this case with an error log of its own.
		return
	}
	installed, err := semver.NewVersion(bv)
	if err != nil {
		return
	}
	stripped, err := installed.SetPrerelease("")
	if err != nil {
		return
	}
	if stripped.LessThan(minimum) {
		ctx.Report(gvk.CustomResourceDefinition,
			msg.NewGatewayAPICRDVersionBelowMinimum(r, name, installed.String(), minimum.String()))
	}
}
