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

package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	mcsapi "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/mcs"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

var serviceExportTimeout = retry.Timeout(time.Second * 2)

func TestServiceExportController(t *testing.T) {
	client := kube.NewFakeClient()

	// Configure the environment with cluster-local hosts.
	m := meshconfig.MeshConfig{
		ServiceSettings: []*meshconfig.MeshConfig_ServiceSettings{
			{
				Settings: &meshconfig.MeshConfig_ServiceSettings_Settings{
					ClusterLocal: true,
				},
				Hosts: []string{"*.unexportable-ns.svc.cluster.local", "unexportable-svc.*.svc.cluster.local"},
			},
		},
	}
	env := model.Environment{Watcher: mesh.NewFixedWatcher(&m)}
	env.Init()

	sc := newAutoServiceExportController(autoServiceExportOptions{
		Client:       client,
		ClusterID:    "",
		DomainSuffix: env.DomainSuffix,
		ClusterLocal: env.ClusterLocal(),
	})

	stop := test.NewStop(t)
	client.RunAndWait(stop)
	sc.Run(stop)

	t.Run("exportable", func(t *testing.T) {
		createSimpleService(t, client.Kube(), "exportable-ns", "foo")
		assertServiceExport(t, client, "exportable-ns", "foo", true)
	})

	t.Run("unexportable", func(t *testing.T) {
		createSimpleService(t, client.Kube(), "unexportable-ns", "foo")
		assertServiceExport(t, client, "unexportable-ns", "foo", false)
	})

	t.Run("no overwrite", func(t *testing.T) {
		// manually create serviceexport
		export := mcsapi.ServiceExport{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ServiceExport",
				APIVersion: features.MCSAPIVersion,
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "exportable-ns",
				Name:      "manual-export",
			},
			Status: mcsapi.ServiceExportStatus{
				Conditions: []mcsapi.ServiceExportCondition{
					{
						Type: mcsapi.ServiceExportValid,
					},
				},
			},
		}

		_, err := client.Dynamic().Resource(mcs.ServiceExportGVR).Namespace("exportable-ns").Create(
			context.TODO(), toUnstructured(&export), metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Unexpected error %v", err)
		}

		// create the associated service
		// no need for assertions, just trying to ensure no errors
		createSimpleService(t, client.Kube(), "exportable-ns", "manual-export")

		// assert that we didn't wipe out the pre-existing serviceexport status
		assertServiceExportHasCondition(t, client, "exportable-ns", "manual-export",
			mcsapi.ServiceExportValid)
	})
}

func createSimpleService(t *testing.T, client kubernetes.Interface, ns string, name string) {
	t.Helper()
	if _, err := client.CoreV1().Services(ns).Create(context.TODO(), &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func assertServiceExport(t *testing.T, client kube.Client, ns, name string, shouldBePresent bool) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		got, err := client.Dynamic().Resource(mcs.ServiceExportGVR).Namespace(ns).Get(context.TODO(), name, metav1.GetOptions{})

		if err != nil && !strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("unexpected error %v", err)
		}
		isPresent := got != nil
		if isPresent != shouldBePresent {
			return fmt.Errorf("unexpected serviceexport state. IsPresent: %v, ShouldBePresent: %v, name: %v, namespace: %v", isPresent, shouldBePresent, name, ns)
		}
		return nil
	}, serviceExportTimeout)
}

func assertServiceExportHasCondition(t *testing.T, client kube.Client, ns, name string, condition mcsapi.ServiceExportConditionType) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		gotU, err := client.Dynamic().Resource(mcs.ServiceExportGVR).Namespace(ns).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("unexpected error %v", err)
		}

		got := &mcsapi.ServiceExport{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(gotU.Object, got); err != nil {
			return err
		}

		if got.Status.Conditions == nil || len(got.Status.Conditions) == 0 || got.Status.Conditions[0].Type != condition {
			return fmt.Errorf("condition incorrect or not found")
		}

		return nil
	}, serviceExportTimeout)
}
