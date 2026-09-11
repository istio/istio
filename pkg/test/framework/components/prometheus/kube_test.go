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

package prometheus

import (
	"strings"
	"testing"
)

func TestGetPrometheusYamlImageOverrides(t *testing.T) {
	prometheusHub := "example.com/cache/prometheus"
	reloaderHub := "example.com/cache/prometheus-operator"
	t.Setenv("PROMETHEUS_HUB", prometheusHub)
	t.Setenv("PROMETHEUS_CONFIG_RELOADER_HUB", reloaderHub)

	yaml, err := getPrometheusYaml()
	if err != nil {
		t.Fatal(err)
	}
	for _, repository := range []string{
		prometheusHub + "/prometheus:",
		reloaderHub + "/prometheus-config-reloader:",
	} {
		if !strings.Contains(yaml, repository) {
			t.Errorf("rendered manifest does not contain overridden repository %q", repository)
		}
	}
	for _, repository := range []string{defaultPrometheusRepository, defaultPrometheusConfigReloaderRepository} {
		if strings.Contains(yaml, repository) {
			t.Errorf("rendered manifest still contains default repository %q", repository)
		}
	}
}
