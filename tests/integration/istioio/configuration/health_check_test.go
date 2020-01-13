// Copyright 2020 Istio Authors
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

package configuration

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/istioio"
)

// https://istio.io/docs/ops/configuration/mesh/app-health-check/
func TestHealthCheck(t *testing.T) {
	framework.
		NewTest(t).
		Run(istioio.NewBuilder("ops__configuration__mesh__app_health_check").
			Add(istioio.Script{
				Input: istioio.Path("scripts/liveness_and_readiness_probes_with_command.txt"),
			}).
			Add(istioio.Script{
				Input: istioio.Path("scripts/liveness_and_readiness_probes_with_http_globally.txt"),
			}).
			Add(istioio.Script{
				Input: istioio.Path("scripts/liveness_and_readiness_probes_with_http_annotations.txt"),
			}).
			Add(istioio.Script{
				Input: istioio.Path("scripts/liveness_and_readiness_probes_with_http_separate_port.txt"),
			}).
			Defer(istioio.Script{
				Input: istioio.Path("scripts/cleanup.txt"),
			}).
			Build())
}
