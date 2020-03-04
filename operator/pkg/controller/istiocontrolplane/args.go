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
	"github.com/spf13/cobra"
)

// Options represents the details used to configure the controller.
type Options struct {
	// BaseChartPath is the abosolute path used as the base path when a relative path is specified in
	// IstioOperator.Spec.ChartPath
	BaseChartPath string
	// DefaultChartPath is the relative path used added to BaseChartPath when no value is specified in
	// IstioOperator.Spec.ChartPath
	DefaultChartPath string
}

// ControllerOptions represents the options used by the controller
var controllerOptions = &Options{
	// XXX: update this once we add charts to the operator
	BaseChartPath:    "/etc/istio-operator/helm",
	DefaultChartPath: "istio",
}

// AttachCobraFlags attaches a set of Cobra flags to the given Cobra command.
//
// Cobra is the command-line processor that Istio uses. This command attaches
// the set of flags used to configure the IstioOperator reconciler
func AttachCobraFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&controllerOptions.BaseChartPath, "base-chart-path", "",
		"The absolute path to a directory containing nested charts, e.g. /etc/istio-operator/helm.  "+
			"This will be used as the base path for any IstioOperator instances specifying a relative ChartPath.")
	cmd.PersistentFlags().StringVar(&controllerOptions.BaseChartPath, "default-chart-path", "",
		"A path relative to base-chart-path containing charts to be used when no ChartPath is specified by an IstioOperator resource, e.g. 1.1.0/istio")
}
