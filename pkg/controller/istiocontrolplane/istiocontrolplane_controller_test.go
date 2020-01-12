// Copyright 2019 Istio Authors
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

package istiocontrolplane

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/kr/pretty"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mesh "istio.io/api/mesh/v1alpha1"
	"istio.io/api/operator/v1alpha1"
	iop "istio.io/operator/pkg/apis/istio/v1alpha1"
	"istio.io/operator/pkg/helmreconciler"
	"istio.io/operator/pkg/name"
)

var (
	healthyVersionStatus = &v1alpha1.InstallStatus_VersionStatus{
		Status:       v1alpha1.InstallStatus_HEALTHY,
		StatusString: "HEALTHY",
	}
	minimalStatus = map[string]*v1alpha1.InstallStatus_VersionStatus{
		string(name.IstioBaseComponentName): healthyVersionStatus,
		string(name.PilotComponentName):     healthyVersionStatus,
	}
	defaultStatus = map[string]*v1alpha1.InstallStatus_VersionStatus{
		string(name.IstioBaseComponentName):       healthyVersionStatus,
		string(name.PilotComponentName):           healthyVersionStatus,
		string(name.SidecarInjectorComponentName): healthyVersionStatus,
		string(name.PolicyComponentName):          healthyVersionStatus,
		string(name.TelemetryComponentName):       healthyVersionStatus,
		string(name.CitadelComponentName):         healthyVersionStatus,
		string(name.GalleyComponentName):          healthyVersionStatus,
		string(name.IngressComponentName):         healthyVersionStatus,
		string(name.AddonComponentName):           healthyVersionStatus,
	}
	demoStatus = map[string]*v1alpha1.InstallStatus_VersionStatus{
		string(name.IstioBaseComponentName):       healthyVersionStatus,
		string(name.PilotComponentName):           healthyVersionStatus,
		string(name.SidecarInjectorComponentName): healthyVersionStatus,
		string(name.PolicyComponentName):          healthyVersionStatus,
		string(name.TelemetryComponentName):       healthyVersionStatus,
		string(name.CitadelComponentName):         healthyVersionStatus,
		string(name.GalleyComponentName):          healthyVersionStatus,
		string(name.IngressComponentName):         healthyVersionStatus,
		string(name.EgressComponentName):          healthyVersionStatus,
		string(name.AddonComponentName):           healthyVersionStatus,
	}
	sdsStatus = map[string]*v1alpha1.InstallStatus_VersionStatus{
		string(name.IstioBaseComponentName):       healthyVersionStatus,
		string(name.PilotComponentName):           healthyVersionStatus,
		string(name.SidecarInjectorComponentName): healthyVersionStatus,
		string(name.PolicyComponentName):          healthyVersionStatus,
		string(name.TelemetryComponentName):       healthyVersionStatus,
		string(name.CitadelComponentName):         healthyVersionStatus,
		string(name.GalleyComponentName):          healthyVersionStatus,
		string(name.NodeAgentComponentName):       healthyVersionStatus,
		string(name.IngressComponentName):         healthyVersionStatus,
		string(name.AddonComponentName):           healthyVersionStatus,
	}
)

type testCase struct {
	description    string
	initialProfile string
	targetProfile  string
}

// TestIOPController_SwitchProfile
func TestIOPController_SwitchProfile(t *testing.T) {
	cases := []testCase{
		{
			description:    "switch profile from minimal to default",
			initialProfile: "minimal",
			targetProfile:  "default",
		},
		{
			description:    "switch profile from default to minimal",
			initialProfile: "default",
			targetProfile:  "minimal",
		},
		{
			description:    "switch profile from default to demo",
			initialProfile: "default",
			targetProfile:  "demo",
		},
		{
			description:    "switch profile from demo to sds",
			initialProfile: "demo",
			targetProfile:  "sds",
		},
		{
			description:    "switch profile from sds to default",
			initialProfile: "sds",
			targetProfile:  "default",
		},
	}
	for i, c := range cases {
		t.Run(strconv.Itoa(i)+":"+c.description, func(t *testing.T) {
			testSwitchProfile(t, c)
		})
	}
}
func testSwitchProfile(t *testing.T, c testCase) {
	t.Helper()
	name := "example-istiocontrolplane"
	namespace := "istio-system"
	iopinstance := &iop.IstioOperator{
		Kind:       "IstioOperator",
		ApiVersion: "install.istio.io/v1alpha1",
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: &v1alpha1.IstioOperatorSpec{
			Profile: c.initialProfile,
			MeshConfig: &mesh.MeshConfig{
				RootNamespace: "istio-system",
			},
		},
	}
	objs := []runtime.Object{
		iopinstance,
	}

	s := scheme.Scheme
	s.AddKnownTypes(iop.SchemeGroupVersion, iopinstance)
	cl := fake.NewFakeClientWithScheme(s, objs...)
	factory := &helmreconciler.Factory{CustomizerFactory: &IstioRenderingCustomizerFactory{}}
	r := &ReconcileIstioOperator{client: cl, scheme: s, factory: factory}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}
	res, err := r.Reconcile(req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}
	// check IOP status
	succeed, err := checkIOPStatus(cl, req.NamespacedName, c.initialProfile)
	if !succeed || err != nil {
		t.Fatalf("failed to get initial expected IstioOperator status: (%v)", err)
	}

	//update IstioOperator : switch profile from minimal to default and reconcile
	err = switchIstioOperatorProfile(cl, req.NamespacedName, c.targetProfile)
	if err != nil {
		t.Fatalf("failed to update IstioOperator: (%v)", err)
	}
	res, err = r.Reconcile(req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}
	if res.Requeue {
		t.Error("reconcile requeue which is not expected")
	}
	// check IOP status
	succeed, err = checkIOPStatus(cl, req.NamespacedName, c.targetProfile)
	if !succeed || err != nil {
		t.Fatalf("failed to get expected target IstioOperator status: (%v)", err)
	}
}

func statusExpected(s1, s2 *v1alpha1.InstallStatus_VersionStatus) bool {
	return s1.Status.String() == s2.Status.String()
}

func switchIstioOperatorProfile(cl client.Client, key client.ObjectKey, profile string) error {
	instance := &iop.IstioOperator{}
	err := cl.Get(context.TODO(), key, instance)
	if err != nil {
		return err
	}
	instance.Spec.Profile = profile
	generation := instance.GetGeneration()
	instance.SetGeneration(generation + 1)
	err = cl.Update(context.TODO(), instance)
	if err != nil {
		return err
	}
	return nil
}
func checkIOPStatus(cl client.Client, key client.ObjectKey, profile string) (bool, error) {
	instance := &iop.IstioOperator{}
	err := cl.Get(context.TODO(), key, instance)
	if err != nil {
		return false, err
	}
	var expectedComponentStatus map[string]*v1alpha1.InstallStatus_VersionStatus
	switch profile {
	case "minimal":
		expectedComponentStatus = minimalStatus
	case "default":
		expectedComponentStatus = defaultStatus
	case "sds":
		expectedComponentStatus = sdsStatus
	case "demo":
		expectedComponentStatus = demoStatus
	}
	status := instance.Status
	size := len(status.ComponentStatus)
	expectedSize := len(expectedComponentStatus)
	if size != expectedSize {
		return false, fmt.Errorf("status got: %s, want: %s", pretty.Sprint(status.ComponentStatus), pretty.Sprint(expectedComponentStatus))
	}
	for k, v := range status.ComponentStatus {
		if s, ok := expectedComponentStatus[k]; ok {
			if !statusExpected(s, v) {
				return false, fmt.Errorf("failed to get Expected IstioOperator status: (%s)", k)
			}
		} else {
			return false, fmt.Errorf("failed to find Expected IstioOperator status: (%s)", k)
		}
	}
	return true, nil
}
