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

package content

import (
	"strings"
	"testing"
)

func TestParamsSetZtunnelStatsPort(t *testing.T) {
	p := &Params{}
	out := p.SetZtunnelStatsPort(15020)
	if out.ZtunnelStatsPort != 15020 {
		t.Errorf("SetZtunnelStatsPort did not set port, got %d", out.ZtunnelStatsPort)
	}
	if p.ZtunnelStatsPort != 0 {
		t.Errorf("SetZtunnelStatsPort mutated receiver, got %d", p.ZtunnelStatsPort)
	}
}

func TestGetZtunnelStatsRequiresStatsPort(t *testing.T) {
	p := &Params{Namespace: "istio-system", Pod: "ztunnel-abc"}
	_, err := GetZtunnelStats(p)
	if err == nil || !strings.Contains(err.Error(), "ztunnel stats port") {
		t.Errorf("expected error about missing stats port, got %v", err)
	}
}

func TestGetZtunnelStatsRequiresNamespaceAndPod(t *testing.T) {
	_, err := GetZtunnelStats(&Params{ZtunnelStatsPort: 15020})
	if err == nil {
		t.Errorf("expected error for missing namespace/pod")
	}
}
