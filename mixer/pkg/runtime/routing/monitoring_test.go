// Copyright 2018 Istio Authors
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

package routing

import (
	"testing"
	"time"

	tpb "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/pkg/runtime/testing/data"
)

func TestDestinationCounters_Update(t *testing.T) {
	reachedEnd := false
	defer func() {
		r := recover()
		if !reachedEnd {
			t.Fatalf("Panic detected: '%v'", r)
		}
	}()

	// Ensure that the call doesn't crash
	serviceConfig := data.ServiceConfig
	table, _ := buildTable(serviceConfig, []string{data.HandlerACheck1, data.InstanceCheck1, data.RuleCheck1}, true)

	d := table.GetDestinations(tpb.TEMPLATE_VARIETY_CHECK, "istio-system")
	d[0].Counters.Update(time.Second, false)
	d[0].Counters.Update(time.Second, true)

	reachedEnd = true
}
