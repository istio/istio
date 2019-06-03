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

package istiocontrolplane

import (
	"istio.io/operator/pkg/apis/istio/v1alpha1"
	"istio.io/operator/pkg/helmreconciler"
)

// defaultProcessingOrder for the rendered charts
var defaultProcessingOrder = []string{
	"istio",
	"istio/charts/security",
	"istio/charts/galley",
	"istio/charts/prometheus",
	"istio/charts/mixer",
	"istio/charts/pilot",
	"istio/charts/gateways",
	"istio/charts/sidecarInjectorWebhook",
	"istio/charts/grafana",
	"istio/charts/tracing",
	"istio/charts/kiali",
}

// IstioRenderingInput is a RenderingInput specific to an IstioControlPlane instance.
type IstioRenderingInput struct {
	instance  *v1alpha1.IstioControlPlane
	chartPath string
}

var _ helmreconciler.RenderingInput = &IstioRenderingInput{}

// NewIstioRenderingInput creates a new IstioRenderiongInput for the specified instance.
func NewIstioRenderingInput(instance *v1alpha1.IstioControlPlane) *IstioRenderingInput {
	return &IstioRenderingInput{instance: instance, chartPath: calculateChartPath(instance.Spec.ChartPath)}
}

// GetChartPath returns the absolute path locating the charts to be rendered.
func (i *IstioRenderingInput) GetChartPath() string {
	return i.chartPath
}

// GetValues returns the values that should be used when rendering the charts.
func (i *IstioRenderingInput) GetValues() map[string]interface{} {
	return i.instance.Spec.RawValues
}

// GetTargetNamespace returns the namespace within which rendered namespaced resources should be generated
// (i.e. Release.Namespace)
func (i *IstioRenderingInput) GetTargetNamespace() string {
	return i.instance.Namespace
}

// GetProcessingOrder returns the order in which the rendered charts should be processed.
func (i *IstioRenderingInput) GetProcessingOrder(manifests helmreconciler.ChartManifestsMap) ([]string, error) {
	seen := map[string]struct{}{}
	ordering := make([]string, 0, len(manifests))
	// known ordering
	for _, chart := range defaultProcessingOrder {
		if _, ok := manifests[chart]; ok {
			ordering = append(ordering, chart)
			seen[chart] = struct{}{}
		}
	}
	// everything else to the end
	for chart := range manifests {
		if _, ok := seen[chart]; !ok {
			ordering = append(ordering, chart)
			seen[chart] = struct{}{}
		}
	}
	return ordering, nil
}
