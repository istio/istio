
// +build integ
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

package analysis

import (
	"context"
	"fmt"
	"istio.io/api/meta/v1alpha1"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

func TestStatusExistsByDefault(t *testing.T) {
	// This test is not yet implemented
	framework.NewTest(t).
		NotImplementedYet(features.Usability_Observability_Status_DefaultExists)
}

func TestAnalysisWritesStatus(t *testing.T) {
	framework.NewTest(t).
		Features(features.Usability_Observability_Status).
		// TODO: make feature labels heirarchical constants like:
		// Label(features.Usability.Observability.Status).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix:   "default",
				Inject:   true,
				Revision: "",
				Labels:   nil,
			})
			// Apply bad config (referencing invalid host)
			ctx.Config().ApplyYAMLOrFail(t, ns.Name(), `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: reviews
spec:
  gateways: [missing-gw]
  hosts:
  - reviews
  http:
  - route:
    - destination: 
        host: reviews
`)
			// Status should report error
			retry.UntilSuccessOrFail(t, func() error {
				return expectVirtualServiceStatus(t, ctx, ns, true)
			}, retry.Timeout(time.Minute*5))
			// Apply config to make this not invalid
			ctx.Config().ApplyYAMLOrFail(t, ns.Name(), `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: missing-gw
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
`)
			// Status should no longer report error
			retry.UntilSuccessOrFail(t, func() error {
				return expectVirtualServiceStatus(t, ctx, ns, false)
			})
		})
}

func TestWorkloadEntryUpdatesStatus(t *testing.T) {
	framework.NewTest(t).
		Features(features.Usability_Observability_Status).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix:   "default",
				Inject:   true,
				Revision: "",
				Labels:   nil,
			})
			// Make a workloadentry
			ctx.Config().ApplyYAMLOrFail(t, ns.Name(), `
apiVersion: networking.istio.io/v1alpha3
kind: WorkloadEntry
metadata:
  name: vm-1
`)
			// Test adding one condition
			stat := &v1alpha1.IstioStatus{
				Conditions: []*v1alpha1.IstioCondition{
					{
						Type: "Health",
						Reason: "DontTellAnyoneButImNotARealReason",
						Status: "True",
					},
					{
						Type: "SomeRandomType",
						Reason: "ImNotHealthSoDontTouchMe",
						Status: "True",
					},
				},
			}
			ctx.Clusters().Default().Istio().NetworkingV1alpha3().WorkloadEntries(ns.Name()).UpdateStatus(context.TODO(), &v1alpha3.WorkloadEntry{Status: *stat}, metav1.UpdateOptions{})
			retry.UntilSuccessOrFail(t, func() error {
				// should update
				return expectWorkloadEntryStatus(t, ctx, ns, stat.Conditions)
			}, retry.Timeout(time.Minute*5))

			// replacing the condition
			for i, cond := range stat.Conditions {
				if cond.Type == "Health" {
					stat.Conditions[i] = &v1alpha1.IstioCondition{
						Type: "Health",
						Reason: "LooksLikeIHavebeenReplaced",
						Status: "False",
					}
				}
			}

			// update this new status
			ctx.Clusters().Default().Istio().NetworkingV1alpha3().WorkloadEntries(ns.Name()).UpdateStatus(context.TODO(), &v1alpha3.WorkloadEntry{Status: *stat}, metav1.UpdateOptions{})
			retry.UntilSuccessOrFail(t, func() error {
				// should update
				return expectWorkloadEntryStatus(t, ctx, ns, stat.Conditions)
			}, retry.Timeout(time.Minute*5))
		})
}

func expectVirtualServiceStatus(t *testing.T, ctx resource.Context, ns namespace.Instance, hasError bool) error {
	c := ctx.Clusters().Default()

	x, err := c.Istio().NetworkingV1alpha3().VirtualServices(ns.Name()).Get(context.TODO(), "reviews", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected test failure: can't get virtualservice: %v", err)
	}

	status := x.Status

	if hasError {
		if len(status.ValidationMessages) < 1 {
			return fmt.Errorf("expected validation messages to exist, but got nothing")
		}
		found := false
		for _, validation := range status.ValidationMessages {
			if validation.Type.Code == msg.ReferencedResourceNotFound.Code() {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("expected error %v to exist", msg.ReferencedResourceNotFound.Code())
		}
	} else if status.ValidationMessages != nil {
		return fmt.Errorf("expected no validation messages, but got %d", len(status.ValidationMessages))
	}

	if len(status.Conditions) < 1 {
		return fmt.Errorf("expected conditions to exist, but got nothing")
	}
	found := false
	for _, condition := range status.Conditions {
		if condition.Type == "Reconciled" {
			found = true
			if condition.Status != "True" {
				return fmt.Errorf("expected Reconciled to be true but was %v", condition.Status)
			}
		}
	}
	if !found {
		return fmt.Errorf("expected Reconciled condition to exist, but got %v", status.Conditions)
	}
	return nil
}

func expectWorkloadEntryStatus(t *testing.T, ctx resource.Context, ns namespace.Instance, expectedConds []*v1alpha1.IstioCondition) error {
	c := ctx.Clusters().Default()

	x, err := c.Istio().NetworkingV1alpha3().WorkloadEntries(ns.Name()).Get(context.TODO(), "vm-1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected test failure: can't get workloadentry: %v", err)
		return err
	}

	status := x.Status

	if !reflect.DeepEqual(status.Conditions, expectedConds){
		t.Errorf("expected conditions %v got %v", expectedConds, status.Conditions)
		return fmt.Errorf("expected conditions %v got %v", expectedConds, status.Conditions)
	}
	return nil
}