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

// lint verifies that all helm values defined in manifests/charts can be
// unmarshalled into the IstioOperator Values struct, ensuring they are
// all defined in the protobuf definitions in operator/pkg/apis.
//
// This uses the same validation logic as istioctl manifest generate.
//
// Usage: go run tools/lint/helm-values-proto/lint.go

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"

	"istio.io/istio/operator/pkg/apis/validation" // nolint: depguard
	"istio.io/istio/operator/pkg/values"          // nolint: depguard
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

// chartToValuesField maps chart paths (relative to manifests/charts) to the
// corresponding field in the Values struct. An empty field path means the chart's
// values go at the top level of spec.values.
//
// Charts not listed here are not validated. In particular, the a-la-carte
// "gateway" chart is intentionally absent: it is not a component of the standard
// istioctl install (see operator/pkg/component.AllComponents), so its values are
// not part of spec.values.
var chartToValuesField = map[string]string{
	"base":                          "",
	"default":                       "",
	"gateways/istio-egress":         "",
	"gateways/istio-ingress":        "",
	"istio-control/istio-discovery": "pilot",
	"istio-cni":                     "cni",
	"ztunnel":                       "ztunnel",
}

// istioOperatorSpecFields go at the top level of spec, not spec.values.
var istioOperatorSpecFields = sets.New(
	"profile", "hub", "tag", "revision", "namespace",
	"compatibilityVersion", "installPackagePath", "meshConfig",
)

// valuesTopLevelFields go at the top level of spec.values.
var valuesTopLevelFields = sets.New(
	"global", "ownerName", "base", "telemetry", "revisionTags",
	"sidecarInjectorWebhook", "gatewayClasses", "gateways", "experimental",
)

func main() {
	// Expect to be run from the repo root.
	if _, err := os.Stat("operator/pkg/apis/values_types.pb.go"); err != nil {
		fmt.Fprintln(os.Stderr, "error: operator/pkg/apis/values_types.pb.go not found; run this script from the repo root")
		os.Exit(1)
	}

	// Merge all chart defaults into the appropriate fields
	mergedIop := map[string]interface{}{
		"apiVersion": "install.istio.io/v1alpha1",
		"kind":       "IstioOperator",
		"metadata": map[string]interface{}{
			"name":      "test",
			"namespace": "istio-system",
		},
		"spec": map[string]interface{}{
			"values": map[string]interface{}{},
		},
	}

	spec := mergedIop["spec"].(map[string]interface{})
	specValues := spec["values"].(map[string]interface{})

	// Iterate in sorted order so that charts contributing to the same field
	// (e.g. "revision", "meshConfig") merge deterministically.
	for _, chartPath := range slices.Sort(maps.Keys(chartToValuesField)) {
		values, err := readChartValues(chartPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: reading chart values: %v\n", err)
			os.Exit(1)
		}
		defaults := extractDefaults(values)
		fieldPath := chartToValuesField[chartPath]

		chartSpecific := map[string]interface{}{}
		for k, v := range defaults {
			switch {
			case istioOperatorSpecFields.Contains(k):
				spec[k] = v
			case fieldPath == "" || valuesTopLevelFields.Contains(k):
				// Multiple charts can contribute to the same top-level field (e.g.
				// "gateways" from the default, pilot and gateway charts). Merge map
				// values instead of overwriting so no contributions are dropped.
				setOrMerge(specValues, k, v)
			default:
				chartSpecific[k] = v
			}
		}

		if fieldPath != "" {
			if _, ok := specValues[fieldPath]; !ok {
				specValues[fieldPath] = map[string]interface{}{}
			}
			specValues[fieldPath] = mergeMaps(specValues[fieldPath].(map[string]interface{}), chartSpecific)
		}
	}

	// Create a values.Map from the merged IstioOperator
	iopYaml, err := yaml.Marshal(mergedIop)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: marshaling merged IstioOperator: %v\n", err)
		os.Exit(1)
	}
	iop, err := values.MapFromYaml(iopYaml)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: creating Map from YAML: %v\n", err)
		os.Exit(1)
	}

	// Validate using the same logic as istioctl
	warnings, errs := validation.ParseAndValidateIstioOperator(iop, nil)
	if errs.ToError() != nil {
		fmt.Println("✗ Found helm values that cannot be unmarshalled into IstioOperator Values struct:")
		fmt.Printf("  - %v\n", errs.ToError())
		fmt.Println("")
		fmt.Println("This usually means you added new helm values in manifests/charts/ that are not")
		fmt.Println("defined in operator/pkg/apis/values_types.proto.")
		fmt.Println("")
		fmt.Println("To fix this, add the missing fields to operator/pkg/apis/values_types.proto,")
		fmt.Println("then regenerate the Go code:")
		fmt.Println("  make operator-proto")
		os.Exit(1)
	}

	if len(warnings) > 0 {
		fmt.Println("⚠ Warnings:")
		for _, w := range warnings {
			fmt.Printf("  - %v\n", w)
		}
	}

	fmt.Println("✓ All helm values can be unmarshalled into IstioOperator Values struct")
	os.Exit(0)
}

// readChartValues reads the values.yaml of the chart at chartPath (relative to
// manifests/charts).
func readChartValues(chartPath string) (map[string]interface{}, error) {
	path := filepath.Join("manifests/charts", chartPath, "values.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var values map[string]interface{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("error: decoding %s: %w", path, err)
	}
	return values, nil
}

// extractDefaults extracts the values from _internal_defaults_do_not_set.
// If not present, returns the original values.
func extractDefaults(values map[string]interface{}) map[string]interface{} {
	if defaults, ok := values["_internal_defaults_do_not_set"]; ok {
		if defaultsMap, ok := defaults.(map[string]interface{}); ok {
			return defaultsMap
		}
	}
	return values
}

// setOrMerge sets m[k] = v, merging if both the existing and new values are maps.
func setOrMerge(m map[string]interface{}, k string, v interface{}) {
	if existing, ok := m[k].(map[string]interface{}); ok {
		if newMap, ok := v.(map[string]interface{}); ok {
			m[k] = mergeMaps(existing, newMap)
			return
		}
	}
	m[k] = v
}

// mergeMaps merges two maps, with the second map taking precedence.
// Nested maps are merged recursively.
func mergeMaps(base, overlay map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(base)+len(overlay))
	maps.Copy(result, base)
	for k, v := range overlay {
		if baseMap, ok := result[k].(map[string]interface{}); ok {
			if overlayMap, ok := v.(map[string]interface{}); ok {
				result[k] = mergeMaps(baseMap, overlayMap)
				continue
			}
		}
		result[k] = v
	}
	return result
}
