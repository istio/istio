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

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	testimage "istio.io/istio/pkg/test/util/image"
	"istio.io/istio/pkg/test/util/yml"
)

func TestGetPrometheusYamlPullSecret(t *testing.T) {
	rendered, err := getPrometheusYaml("private-registry")
	if err != nil {
		t.Fatal(err)
	}
	var serviceAccount corev1.ServiceAccount
	if err := yaml.Unmarshal([]byte(yml.SplitString(rendered)[0]), &serviceAccount); err != nil {
		t.Fatal(err)
	}
	if len(serviceAccount.ImagePullSecrets) != 1 || serviceAccount.ImagePullSecrets[0].Name != "private-registry" {
		t.Fatalf("configured pull secret missing from Prometheus service account: %v", serviceAccount.ImagePullSecrets)
	}
}

func TestGetPrometheusYamlImageDefaults(t *testing.T) {
	yaml, err := getPrometheusYaml("")
	if err != nil {
		t.Fatal(err)
	}
	for _, repository := range []string{
		"quay.io/prometheus/prometheus",
		"ghcr.io/prometheus-operator/prometheus-config-reloader",
	} {
		if !strings.Contains(yaml, repository) {
			t.Errorf("rendered manifest does not contain default repository %q", repository)
		}
	}
}

func TestGetPrometheusYamlImageOverrides(t *testing.T) {
	prometheusHub := "example.com/cache/quay"
	reloaderHub := "example.com/cache/ghcr"
	t.Setenv("QUAY_REGISTRY", prometheusHub)
	t.Setenv("GHCR_REGISTRY", reloaderHub)

	yaml, err := getPrometheusYaml("")
	if err != nil {
		t.Fatal(err)
	}
	for _, repository := range []string{
		prometheusHub + "/prometheus/prometheus:",
		reloaderHub + "/prometheus-operator/prometheus-config-reloader:",
	} {
		if !strings.Contains(yaml, repository) {
			t.Errorf("rendered manifest does not contain overridden repository %q", repository)
		}
	}
	for _, repository := range []string{
		testimage.QuayRegistry + "/" + defaultPrometheusRepository,
		testimage.GHCRRegistry + "/" + defaultConfigReloaderRepository,
	} {
		if strings.Contains(yaml, repository) {
			t.Errorf("rendered manifest still contains default repository %q", repository)
		}
	}
}
