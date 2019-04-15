// Copyright 2019 Istio Authors.
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

package install

import (
	"fmt"
	"testing"

	"k8s.io/api/extensions/v1beta1"
)

var (
	availableDeployment = v1beta1.Deployment{
		Status: v1beta1.DeploymentStatus{
			Conditions: []v1beta1.DeploymentCondition{
				{
					Type: v1beta1.DeploymentAvailable,
				},
			},
		},
	}

	scaleUpRollingDeployment = v1beta1.Deployment{
		Spec: v1beta1.DeploymentSpec{
			Replicas: &[]int32{3}[0],
		},
		Status: v1beta1.DeploymentStatus{
			Conditions: []v1beta1.DeploymentCondition{
				{
					Type: v1beta1.DeploymentProgressing,
				},
				{
					Type: v1beta1.DeploymentAvailable,
				},
			},
			UpdatedReplicas: 2,
		},
	}

	deletingOldRollingDeployment = v1beta1.Deployment{
		Spec: v1beta1.DeploymentSpec{
			Replicas: &[]int32{2}[0],
		},
		Status: v1beta1.DeploymentStatus{
			Conditions: []v1beta1.DeploymentCondition{
				{
					Type: v1beta1.DeploymentProgressing,
				},
				{
					Type: v1beta1.DeploymentAvailable,
				},
			},
			UpdatedReplicas:   2,
			AvailableReplicas: 2,
			Replicas:          3,
		},
	}

	failedDeployment = v1beta1.Deployment{
		Spec: v1beta1.DeploymentSpec{
			Replicas: &[]int32{2}[0],
		},
		Status: v1beta1.DeploymentStatus{
			Conditions: []v1beta1.DeploymentCondition{
				{
					Type: v1beta1.DeploymentReplicaFailure,
				},
			},
			UpdatedReplicas:   2,
			AvailableReplicas: 0,
			Replicas:          3,
		},
	}

	deadlineExceededDeployment = v1beta1.Deployment{
		Status: v1beta1.DeploymentStatus{
			Conditions: []v1beta1.DeploymentCondition{
				{
					Type:   v1beta1.DeploymentProgressing,
					Reason: "ProgressDeadlineExceeded",
				},
			},
		},
	}
)

func TestGetDeploymentStatus(t *testing.T) {
	errCases := []*v1beta1.Deployment{
		&scaleUpRollingDeployment,
		&deletingOldRollingDeployment,
		&failedDeployment,
		&deadlineExceededDeployment,
	}
	for i, c := range errCases {
		t.Run(fmt.Sprintf("[err-%v] ", i), func(tt *testing.T) {
			if err := getDeploymentStatus(c, "fooDeploy", ""); err == nil {
				tt.Fatalf("unexpected nil error")
			}
		})
	}

	okCases := []*v1beta1.Deployment{
		&availableDeployment,
	}
	for i, c := range okCases {
		t.Run(fmt.Sprintf("[ok-%v] ", i), func(tt *testing.T) {
			if err := getDeploymentStatus(c, "fooDeploy", ""); err != nil {
				tt.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestGetDeploymentCondition(t *testing.T) {
	cases := []struct {
		status     v1beta1.DeploymentStatus
		condType   v1beta1.DeploymentConditionType
		shouldFind bool
	}{
		{
			// Simple "find Available in Available"
			status:     availableDeployment.Status,
			condType:   v1beta1.DeploymentAvailable,
			shouldFind: true,
		},
		{
			// find Available in Progressing,Available
			// valid in e.g. RollingUpdate
			status:     scaleUpRollingDeployment.Status,
			condType:   v1beta1.DeploymentAvailable,
			shouldFind: true,
		},
		{
			// find Available in ReplicaFailure
			status:     failedDeployment.Status,
			condType:   v1beta1.DeploymentAvailable,
			shouldFind: false,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v] ", i), func(tt *testing.T) {
			dc := getDeploymentCondition(c.status, c.condType)
			if c.shouldFind == false {
				if dc != nil {
					tt.Fatalf("unexpected condition: got %v want nil", dc)
				}
			} else {
				if dc.Type != c.condType {
					tt.Fatalf("unexpected condition: got %v want %v", dc, c.condType)
				}
			}
		})
	}
}

func TestFindResourceInSpec(t *testing.T) {
	cases := []struct {
		kind   string
		plural string
	}{
		{
			// Should find Istio resourcespecs
			kind:   "Policy",
			plural: "policies",
		},
		{
			// Should find Kubernetes resourcespecs
			kind:   "Service",
			plural: "services",
		},
		{
			// Should be empty for not-found
			kind:   "ThisIsNotAKubernetesResourceSpecKind",
			plural: "",
		},
		{
			// Should be empty for empty input
			kind:   "",
			plural: "",
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v] %v ", i, c.kind), func(tt *testing.T) {
			plural := findResourceInSpec(c.kind)
			if plural != c.plural {
				tt.Fatalf("unexpected plural from kind: got %v want %v", plural, c.plural)
			}
		})
	}
}
