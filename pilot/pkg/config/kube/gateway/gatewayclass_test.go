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

package gateway

import (
	"context"
	"fmt"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gateway "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

func TestClassController(t *testing.T) {
	client := kube.NewFakeClient()
	cc := NewClassController(client)
	stop := test.NewStop(t)
	client.RunAndWait(stop)
	go cc.Run(stop)
	createClass := func(name, controller string) {
		gc := &gateway.GatewayClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: gateway.GatewayClassSpec{
				ControllerName: gateway.GatewayController(controller),
			},
		}
		_, err := client.GatewayAPI().GatewayV1alpha2().GatewayClasses().Create(context.Background(), gc, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			_, _ = client.GatewayAPI().GatewayV1alpha2().GatewayClasses().Update(context.Background(), gc, metav1.UpdateOptions{})
		}
	}
	deleteClass := func(name string) {
		client.GatewayAPI().GatewayV1alpha2().GatewayClasses().Delete(context.Background(), name, metav1.DeleteOptions{})
	}
	expectClass := func(name, controller string) {
		t.Helper()
		retry.UntilSuccessOrFail(t, func() error {
			gc, err := client.GatewayAPI().GatewayV1alpha2().GatewayClasses().Get(context.Background(), name, metav1.GetOptions{})
			if controllers.IgnoreNotFound(err) != nil {
				return err
			}
			if controller == "" {
				if gc == nil { // Expect none, got none
					return nil
				}
				return fmt.Errorf("expected no class, got %v", gc.Spec.ControllerName)
			}
			if gc == nil {
				return fmt.Errorf("expected class %v, got none", controller)
			}
			if gateway.GatewayController(controller) != gc.Spec.ControllerName {
				return fmt.Errorf("expected class %v, got %v", controller, gc.Spec.ControllerName)
			}
			return nil
		}, retry.Timeout(time.Second*3))
	}

	// Class should be created initially
	expectClass(DefaultClassName, ControllerName)

	// Once we delete it, it should be added back
	deleteClass(DefaultClassName)
	expectClass(DefaultClassName, ControllerName)

	// Overwrite the class, controller should not reconcile it back
	createClass(DefaultClassName, "different-controller")
	expectClass(DefaultClassName, "different-controller")

	// Once we delete it, it should be added back
	deleteClass(DefaultClassName)
	expectClass(DefaultClassName, ControllerName)

	// Create an unrelated GatewayClass, we should not do anything to it
	createClass("something-else", "different-controller")
	expectClass("something-else", "different-controller")
	deleteClass("something-else")
	expectClass("something-else", "")
}
