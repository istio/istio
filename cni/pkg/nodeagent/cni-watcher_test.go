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

package nodeagent

import (
	"encoding/json"
	"testing"

	"istio.io/istio/pkg/test/util/assert"
)

func TestProcessAddEventGoodPayload(t *testing.T) {
	valid := CNIPluginAddEvent{
		Netns:        "/var/netns/foo",
		PodName:      "pod-bingo",
		PodNamespace: "funkyns",
	}

	payload, _ := json.Marshal(valid)

	addEvent, err := processAddEvent(payload)

	assert.NoError(t, err)
	assert.Equal(t, valid, addEvent)
}

func TestProcessAddEventBadPayload(t *testing.T) {
	valid := CNIPluginAddEvent{
		Netns:        "/var/netns/foo",
		PodName:      "pod-bingo",
		PodNamespace: "funkyns",
	}

	payload, _ := json.Marshal(valid)

	invalid := string(payload) + "funkyjunk"

	_, err := processAddEvent([]byte(invalid))

	assert.Error(t, err)
}
