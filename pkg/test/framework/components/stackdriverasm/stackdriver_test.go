//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package stackdriverasm

import (
	"testing"
)

func TestResourceFilterParam_String(t *testing.T) {
	for param, exp := range map[ResourceFilterParam]string{
		// container metrics
		{FilterFor: "log", ResourceType: ContainerResourceType, WorkloadName: "wl"}: `resource.type = "k8s_container" AND resource.labels.pod_name = "wl"`,
		{FilterFor: "log", ResourceType: ContainerResourceType, Namespace: "ns1"}:   `resource.type = "k8s_container" AND resource.labels.namespace_name = "ns1"`,
		// container logs are the same as metrics
		{ResourceType: ContainerResourceType, WorkloadName: "wl"}: `resource.type = "k8s_container" AND resource.labels.pod_name = "wl"`,
		{ResourceType: ContainerResourceType, Namespace: "ns1"}:   `resource.type = "k8s_container" AND resource.labels.namespace_name = "ns1"`,

		// VM metrics use different labels
		{ResourceType: VMResourceType, WorkloadName: "wl"}: `resource.type = "gce_instance" AND metric.labels.source_workload_name = "wl"`,
		{ResourceType: VMResourceType, Namespace: "ns1"}:   `resource.type = "gce_instance" AND metric.labels.source_workload_namespace = "ns1"`,

		// VM logs use different labels from VM merics
		{FilterFor: "log", ResourceType: VMResourceType, WorkloadName: "wl"}: `resource.type = "gce_instance" AND labels.source_workload = "wl"`,
		{FilterFor: "log", ResourceType: VMResourceType, Namespace: "ns1"}:   `resource.type = "gce_instance" AND labels.source_namespace = "ns1"`,
	} {
		t.Run(exp, func(t *testing.T) {
			if got := param.String(); exp != got {
				t.Errorf("want %q but got %q", exp, got)
			}
		})
	}
}
