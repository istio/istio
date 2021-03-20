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

package crdclient

import (
	"istio.io/pkg/monitoring"
)

var (
	typeTag  = monitoring.MustCreateLabel("type")
	eventTag = monitoring.MustCreateLabel("event")

	k8sEvents = monitoring.NewSum(
		"pilot_k8s_cfg_events",
		"Events from k8s config.",
		monitoring.WithLabels(typeTag, eventTag),
	)
)

func init() {
	monitoring.MustRegister(k8sEvents)
}

func incrementEvent(kind, event string) {
	k8sEvents.With(typeTag.Value(kind), eventTag.Value(event)).Increment()
}
