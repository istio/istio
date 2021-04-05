// +build integ
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

package pilot

import (
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/shell"
)

func TestPiggyback(t *testing.T) {
	framework.
		NewTest(t).Features("usability.observability.proxy-status"). // TODO create new "agent-piggyback" feature
		RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			execCmd := fmt.Sprintf(
				"kubectl -n %s exec %s -c istio-proxy -- curl localhost:15014/debug/syncz",
				apps.PodA[0].Config().Namespace.Name(),
				apps.PodA[0].WorkloadsOrFail(t)[0].PodName())
			out, err := shell.Execute(false, execCmd)
			if err != nil {
				t.Fatalf("couldn't curl sidecar: %v", err)
			}
			if !strings.Contains(out, "\"typeUrl\": \"istio.io/debug/syncz\"") {
				t.Fatalf("the output doesn't contain expected typeURL: %s", out)
			}
			if !strings.Contains(out, "\"xdsConfig\"") {
				t.Fatalf("the output didn't unmarshal as expected (no xdsConfig): %s", out)
			}
		})
}
