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

package opentelemetry

import (
	"strings"
	"testing"

	testimage "istio.io/istio/pkg/test/util/image"
)

func TestGetYamlImageOverride(t *testing.T) {
	hub := "example.com/cache/docker"
	t.Setenv("DOCKER_REGISTRY", hub)

	yaml, err := getYaml()
	if err != nil {
		t.Fatal(err)
	}
	repository := hub + "/otel/opentelemetry-collector-contrib:"
	if !strings.Contains(yaml, repository) {
		t.Errorf("rendered manifest does not contain overridden repository %q", repository)
	}
	defaultRepository := testimage.DockerRegistry + "/" + defaultOtelCollectorRepository
	if strings.Contains(yaml, defaultRepository) {
		t.Errorf("rendered manifest still contains default repository %q", defaultRepository)
	}
}
