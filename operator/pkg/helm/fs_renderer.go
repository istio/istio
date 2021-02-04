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

package helm

import (
	"fmt"
	"os"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
)

// FileTemplateRenderer is a helm template renderer for a local filesystem.
type FileTemplateRenderer struct {
	namespace        string
	componentName    string
	helmChartDirPath string
	chart            *chart.Chart
	started          bool
}

// NewFileTemplateRenderer creates a TemplateRenderer with the given parameters and returns a pointer to it.
// helmChartDirPath must be an absolute file path to the root of the helm charts.
func NewFileTemplateRenderer(helmChartDirPath, componentName, namespace string) *FileTemplateRenderer {
	return &FileTemplateRenderer{
		namespace:        namespace,
		componentName:    componentName,
		helmChartDirPath: helmChartDirPath,
	}
}

// Run implements the TemplateRenderer interface.
func (h *FileTemplateRenderer) Run() error {
	if err := h.loadChart(); err != nil {
		return err
	}

	h.started = true
	return nil
}

// RenderManifest renders the current helm templates with the current values and returns the resulting YAML manifest string.
func (h *FileTemplateRenderer) RenderManifest(values string) (string, error) {
	if !h.started {
		return "", fmt.Errorf("fileTemplateRenderer for %s not started in renderChart", h.componentName)
	}
	return renderChart(h.namespace, values, h.chart, nil)
}

// RenderManifestFiltered filters templates to render using the supplied filter function.
func (h *FileTemplateRenderer) RenderManifestFiltered(values string, filter TemplateFilterFunc) (string, error) {
	if !h.started {
		return "", fmt.Errorf("fileTemplateRenderer for %s not started in renderChart", h.componentName)
	}
	return renderChart(h.namespace, values, h.chart, filter)
}

var removedComponents = map[string]struct{}{
	"prometheus": {},
	"grafana":    {},
	"tracing":    {},
	"kiali":      {},
}

func missingComponentMessages(component string) error {
	if _, f := removedComponents[component]; f {
		// nolint: lll
		return fmt.Errorf("component %q is not longer supported. Please remove it from the addonComponent configuration. See https://istio.io/latest/blog/2020/addon-rework/ for more info", component)
	}
	return fmt.Errorf("component %q does not exist", component)
}

// loadChart implements the TemplateRenderer interface.
func (h *FileTemplateRenderer) loadChart() error {
	var err error
	if h.chart, err = loader.Load(h.helmChartDirPath); err != nil {
		if os.IsNotExist(err) {
			return missingComponentMessages(h.componentName)
		}
		return err
	}
	return nil
}
