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
	"fmt"

	"github.com/hashicorp/go-multierror"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8s "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/util/istiomultierror"
)

// ClassController is a controller that creates the default Istio GatewayClass(s). This will not
// continually reconcile the full state of the GatewayClass object, and instead only create the class
// if it doesn't exist. This allows users to manage it through other means or modify it as they wish.
// If it is deleted, however, it will be added back.
// This controller intentionally does not do leader election for simplicity. Because we only create
// and not update there is no need; the first controller to create the GatewayClass wins.
type ClassController struct {
	queue   controllers.Queue
	classes kclient.Client[*gateway.GatewayClass]
}

func NewClassController(kc kube.Client) *ClassController {
	gc := &ClassController{}
	gc.queue = controllers.NewQueue("gateway class",
		controllers.WithReconciler(gc.Reconcile),
		controllers.WithMaxAttempts(25))

	gc.classes = kclient.New[*gateway.GatewayClass](kc)
	gc.classes.AddEventHandler(controllers.FilteredObjectHandler(gc.queue.AddObject, func(o controllers.Object) bool {
		_, f := classInfos[o.GetName()]
		return f
	}))
	return gc
}

func (c *ClassController) Run(stop <-chan struct{}) {
	// Ensure we initially reconcile the current state
	c.queue.Add(types.NamespacedName{})
	c.queue.Run(stop)
}

func (c *ClassController) Reconcile(types.NamespacedName) error {
	err := istiomultierror.New()
	for class := range classInfos {
		err = multierror.Append(err, c.reconcileClass(class))
	}
	return err.ErrorOrNil()
}

func (c *ClassController) reconcileClass(class string) error {
	if c.classes.Get(class, "") != nil {
		log.Debugf("GatewayClass/%v already exists, no action", class)
		return nil
	}
	classInfo := classInfos[class]
	gc := &gateway.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: class,
		},
		Spec: gateway.GatewayClassSpec{
			ControllerName: gateway.GatewayController(classInfo.controller),
			Description:    &classInfo.description,
		},
	}
	var err error
	gc, err = c.classes.Create(gc)
	if err != nil && !kerrors.IsConflict(err) {
		return err
	} else if err != nil && kerrors.IsConflict(err) {
		// This is not really an error, just a race condition
		log.Infof("Attempted to create GatewayClass/%v, but it was already created", class)
	}
	if err != nil {
		return err
	}
	if !classInfo.reportGatewayClassStatus {
		return nil
	}
	gc.Status = GetClassStatus(&gc.Status, gc.Generation)
	if _, err := c.classes.UpdateStatus(gc); err != nil {
		return fmt.Errorf("failed to update status: %v", err)
	}
	return err
}

func GetClassStatus(existing *k8s.GatewayClassStatus, gen int64) k8s.GatewayClassStatus {
	if existing == nil {
		existing = &k8s.GatewayClassStatus{}
	}
	existing.Conditions = kstatus.UpdateConditionIfChanged(existing.Conditions, metav1.Condition{
		Type:               string(gateway.GatewayClassConditionStatusAccepted),
		Status:             kstatus.StatusTrue,
		ObservedGeneration: gen,
		LastTransitionTime: metav1.Now(),
		Reason:             string(gateway.GatewayClassConditionStatusAccepted),
		Message:            "Handled by Istio controller",
	})
	return *existing
}
