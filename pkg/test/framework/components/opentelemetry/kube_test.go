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
)

func TestGetYamlImageOverride(t *testing.T) {
	hub := "example.com/cache/otel"
	t.Setenv("OTEL_HUB", hub)

	yaml, err := getYaml()
	if err != nil {
		t.Fatal(err)
	}
	repository := hub + "/opentelemetry-collector-contrib:"
	if !strings.Contains(yaml, repository) {
		t.Errorf("rendered manifest does not contain overridden repository %q", repository)
	}
	if strings.Contains(yaml, defaultOtelCollectorRepository) {
		t.Errorf("rendered manifest still contains default repository %q", defaultOtelCollectorRepository)
	}
}
