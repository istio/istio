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
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"
	"sigs.k8s.io/mcs-api/pkg/client/clientset/versioned"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/util/retry"
)

func TestServiceExportController(t *testing.T) {
	client := kube.NewFakeClient()

	//create two services, one for export and one not
	createSimpleService(t, client, "ns2", "foo")
	createSimpleService(t, client, "ns1", "foo")

	//start the controller
	hosts := []string{"*.ns1.svc.cluster.local", "secretservice.*.svc.cluster.local", "service12.ns12.svc.cluster.local"}
	pushContext := model.PushContext{}
	pushContext.SetClusterLocalHosts(hosts)

	sc, _ := NewServiceExportController(client, &pushContext)

	stop := make(chan struct{})
	client.RunAndWait(stop)
	sc.Run(stop)

	//assert that the exportable service has a serviceexport and the other doesn't
	//(testing startup sync)
	assertServiceExport(t, client.MCSApis(), "ns2", "foo", true)
	assertServiceExport(t, client.MCSApis(), "ns1", "foo", false)

	//add exportable service
	createSimpleService(t, client, "ns3", "bar")

	//assert that the service export is created
	assertServiceExport(t, client.MCSApis(), "ns3", "bar", true)

	//add un-exportable service
	createSimpleService(t, client, "ns4", "secretservice")

	//assert that the service export is not created
	assertServiceExport(t, client.MCSApis(), "ns4", "secretservice", false)

	//delete exportable service
	deleteSimpleService(t, client, "ns2", "foo")

	//ensure serviceexport is deleted
	assertServiceExport(t, client.MCSApis(), "ns2", "foo", false)

	//manually create serviceexport
	export := v1alpha1.ServiceExport{
		Status: v1alpha1.ServiceExportStatus{
			Conditions: []v1alpha1.ServiceExportCondition{
				{
					Type: v1alpha1.ServiceExportValid,
				},
			},
		},
	}

	export.Name = "another-export"
	export.Namespace = "ns5"
	_, err := client.MCSApis().MulticlusterV1alpha1().ServiceExports("ns5").Create(context.TODO(), &export, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}

	//create the associated service
	//no need for assertions, just trying to ensure no errors
	createSimpleService(t, client, "ns5", "another-export")

	//assert that we didn't wipe out the pre-existing serviceexport status
	assertServiceExportHasCondition(t, client.MCSApis(), "ns5", "another-export", v1alpha1.ServiceExportValid)

	//delete un-exportable service
	//trying to ensure no errors
	deleteSimpleService(t, client, "ns1", "foo")

	//manually delete arbitrary serviceexport
	err = client.MCSApis().MulticlusterV1alpha1().ServiceExports("ns3").Delete(context.TODO(), "bar", metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}

	//delete associated service
	//again, just making sure no errors
	deleteSimpleService(t, client, "ns3", "bar")

}

func createSimpleService(t *testing.T, client kubernetes.Interface, ns string, name string) {
	t.Helper()
	if _, err := client.CoreV1().Services(ns).Create(context.TODO(), &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func deleteSimpleService(t *testing.T, client kubernetes.Interface, ns string, name string) {
	err := client.CoreV1().Services(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}
}

func assertServiceExport(t *testing.T, client versioned.Interface, ns, name string, shouldBePresent bool) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		got, err := client.MulticlusterV1alpha1().ServiceExports(ns).Get(context.TODO(), name, metav1.GetOptions{})

		if err != nil && !strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("unexpected error %v", err)
		}
		isPresent := got != nil
		if isPresent != shouldBePresent {
			return fmt.Errorf("unexpected serviceexport state. IsPresent: %v, ShouldBePresent: %v, name: %v, namespace: %v", isPresent, shouldBePresent, name, ns)
		}
		return nil
	}, retry.Timeout(time.Second*2))
}

func assertServiceExportHasCondition(t *testing.T, client versioned.Interface, ns, name string, condition v1alpha1.ServiceExportConditionType) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		got, err := client.MulticlusterV1alpha1().ServiceExports(ns).Get(context.TODO(), name, metav1.GetOptions{})

		if err != nil {
			return fmt.Errorf("unexpected error %v", err)
		}

		if got.Status.Conditions == nil || len(got.Status.Conditions) == 0 || got.Status.Conditions[0].Type != condition {
			return fmt.Errorf("condition incorrect or not found")
		}

		return nil
	}, retry.Timeout(time.Second*2))
}
