// Copyright Istio Authors.
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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"istio.io/api/operator/v1alpha1"

	operatorv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
)

var (
	availableDeployment = appsv1.Deployment{
		Status: appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{
				{
					Type: appsv1.DeploymentAvailable,
				},
			},
		},
	}

	scaleUpRollingDeployment = appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Replicas: &[]int32{3}[0],
		},
		Status: appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{
				{
					Type: appsv1.DeploymentProgressing,
				},
				{
					Type: appsv1.DeploymentAvailable,
				},
			},
			UpdatedReplicas: 2,
		},
	}

	deletingOldRollingDeployment = appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Replicas: &[]int32{2}[0],
		},
		Status: appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{
				{
					Type: appsv1.DeploymentProgressing,
				},
				{
					Type: appsv1.DeploymentAvailable,
				},
			},
			UpdatedReplicas:   2,
			AvailableReplicas: 2,
			Replicas:          3,
		},
	}

	failedDeployment = appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Replicas: &[]int32{2}[0],
		},
		Status: appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{
				{
					Type: appsv1.DeploymentReplicaFailure,
				},
			},
			UpdatedReplicas:   2,
			AvailableReplicas: 0,
			Replicas:          3,
		},
	}

	deadlineExceededDeployment = appsv1.Deployment{
		Status: appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentProgressing,
					Reason: "ProgressDeadlineExceeded",
				},
			},
		},
	}

	sampleIOP = &operatorv1alpha1.IstioOperator{
		TypeMeta: metav1.TypeMeta{
			Kind:       "IstioOperator",
			APIVersion: "install.istio.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "installed-state",
			Namespace: "istio-system",
			ManagedFields: []metav1.ManagedFieldsEntry{
				{
					FieldsType: "FieldsV1",
					FieldsV1: &metav1.FieldsV1{
						Raw: []byte(`{"f:metadata": {}}`),
					},
					Manager:   "istioctl",
					Operation: metav1.ManagedFieldsOperationUpdate,
					Time:      &metav1.Time{Time: time.Now()},
				},
			},
			CreationTimestamp: metav1.Time{Time: time.Now()},
		},
		Spec: &v1alpha1.IstioOperatorSpec{Profile: "default"},
		Status: &v1alpha1.InstallStatus{
			Status: v1alpha1.InstallStatus_HEALTHY,
			ComponentStatus: map[string]*v1alpha1.InstallStatus_VersionStatus{
				"Base": {
					Status: v1alpha1.InstallStatus_HEALTHY,
				},
			},
		},
	}
)

func TestGetDeploymentStatus(t *testing.T) {
	errCases := []*appsv1.Deployment{
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

	okCases := []*appsv1.Deployment{
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
		status     appsv1.DeploymentStatus
		condType   appsv1.DeploymentConditionType
		shouldFind bool
	}{
		{
			// Simple "find Available in Available"
			status:     availableDeployment.Status,
			condType:   appsv1.DeploymentAvailable,
			shouldFind: true,
		},
		{
			// find Available in Progressing,Available
			// valid in e.g. RollingUpdate
			status:     scaleUpRollingDeployment.Status,
			condType:   appsv1.DeploymentAvailable,
			shouldFind: true,
		},
		{
			// find Available in ReplicaFailure
			status:     failedDeployment.Status,
			condType:   appsv1.DeploymentAvailable,
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

func TestIstioOperatorConversion(t *testing.T) {
	unObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(sampleIOP)
	if err != nil {
		t.Fatal(err)
	}

	out, err := convertToIstioOperator(unObj)
	if err != nil {
		t.Fatal(err)
	}

	if out.Status.Status != v1alpha1.InstallStatus_HEALTHY || out.GetCreationTimestamp().Unix() == 0 {
		t.Fatal("Failed to unmarshal")
	}
}

func BenchmarkConvertIstioOperator(b *testing.B) {
	unObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(sampleIOP)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := convertToIstioOperator(unObj)
		if err != nil {
			b.Fatal(err)
		}
	}
}
