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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gateway "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/typed/apis/v1alpha2"
	lister "sigs.k8s.io/gateway-api/pkg/client/listers/apis/v1alpha2"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
)

// ClassController is a controller that creates the default Istio GatewayClass. This will not
// continually reconcile the full state of the GatewayClass object, and instead only create the class
// if it doesn't exist. This allows users to manage it through other means or modify it as they wish.
// If it is deleted, however, it will be added back.
// This controller intentionally does not do leader election for simplicity. Because we only create
// and not update there is no need; the first controller to create the GatewayClass wins.
type ClassController struct {
	queue        controllers.Queue
	classes      lister.GatewayClassLister
	directClient gatewayclient.GatewayClassInterface
}

func NewClassController(client kube.Client) *ClassController {
	gc := &ClassController{}
	gc.queue = controllers.NewQueue("gateway class",
		controllers.WithReconciler(gc.Reconcile),
		controllers.WithMaxAttempts(25))

	class := client.GatewayAPIInformer().Gateway().V1alpha2().GatewayClasses()
	gc.classes = class.Lister()
	gc.directClient = client.GatewayAPI().GatewayV1alpha2().GatewayClasses()
	class.Informer().
		AddEventHandler(controllers.FilteredObjectHandler(gc.queue.AddObject, func(o controllers.Object) bool {
			return o.GetName() == DefaultClassName
		}))
	return gc
}

func (c *ClassController) Run(stop <-chan struct{}) {
	// Ensure we initially reconcile the current state
	c.queue.Add(types.NamespacedName{Name: DefaultClassName})
	c.queue.Run(stop)
}

func (c *ClassController) Reconcile(name types.NamespacedName) error {
	_, err := c.classes.Get(DefaultClassName)
	if err := controllers.IgnoreNotFound(err); err != nil {
		log.Errorf("unable to fetch GatewayClass: %v", err)
		return err
	}
	if !apierrors.IsNotFound(err) {
		log.Debugf("GatewayClass/%v already exists, no action", DefaultClassName)
		return nil
	}
	desc := "The default Istio GatewayClass"
	gc := &gateway.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: DefaultClassName,
		},
		Spec: gateway.GatewayClassSpec{
			ControllerName: ControllerName,
			Description:    &desc,
		},
	}
	_, err = c.directClient.Create(context.Background(), gc, metav1.CreateOptions{})
	if apierrors.IsConflict(err) {
		// This is not really an error, just a race condition
		log.Infof("Attempted to create GatewayClass/%v, but it was already created", DefaultClassName)
		return nil
	}
	return err
}
