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

package agentgateway

import (
	"github.com/hashicorp/go-multierror"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/istiomultierror"
)

var log = istiolog.RegisterScope("gateway", "gateway-api controller")

// ClassController is a controller that creates the agentgateway GatewayClass. This will not
// continually reconcile the full state of the GatewayClass object, and instead only create the class
// if it doesn't exist. This allows users to manage it through other means or modify it as they wish.
// If it is deleted, however, it will be added back.
// This controller intentionally does not do leader election for simplicity. Because we only create
// and not update there is no need; the first controller to create the GatewayClass wins.
type ClassController struct {
	queue          controllers.Queue
	classes        kclient.Client[*k8sv1.GatewayClass]
	builtinClasses map[k8sv1.ObjectName]k8sv1.GatewayController
	classInfos     map[k8sv1.GatewayController]gatewaycommon.ClassInfo
}

// ClassControllerOptions configures the ClassController
type ClassControllerOptions struct {
	// BuiltinClasses maps class names to their controller names
	// If nil, uses the default BuiltinClasses
	BuiltinClasses map[k8sv1.ObjectName]k8sv1.GatewayController
	// ClassInfos maps controller names to their class info
	// If nil, uses the default ClassInfos
	ClassInfos map[k8sv1.GatewayController]gatewaycommon.ClassInfo
}

func NewAgentgatewayClassController(kc kube.Client, opts ClassControllerOptions) *ClassController {
	builtinClasses := opts.BuiltinClasses
	if builtinClasses == nil {
		builtinClasses = gatewaycommon.AgentgatewayBuiltinClasses
	}
	classInfos := opts.ClassInfos
	if classInfos == nil {
		classInfos = gatewaycommon.AgentgatewayClassInfos
	}
	gc := &ClassController{
		builtinClasses: builtinClasses,
		classInfos:     classInfos,
	}
	gc.queue = controllers.NewQueue("agentgateway gateway class",
		controllers.WithReconciler(gc.Reconcile),
		controllers.WithMaxAttempts(25))

	gc.classes = kclient.New[*k8sv1.GatewayClass](kc)
	gc.classes.AddEventHandler(controllers.FilteredObjectHandler(gc.queue.AddObject, func(o controllers.Object) bool {
		_, f := gc.builtinClasses[k8sv1.ObjectName(o.GetName())]
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
	for class := range c.builtinClasses {
		err = multierror.Append(err, c.reconcileClass(class))
	}
	return err.ErrorOrNil()
}

func (c *ClassController) reconcileClass(class k8sv1.ObjectName) error {
	if c.classes.Get(string(class), "") != nil {
		log.Debugf("GatewayClass/%v already exists, no action", class)
		return nil
	}
	controller := c.builtinClasses[class]
	classInfo, f := c.classInfos[controller]
	if !f {
		// Should only happen when ambient is disabled; otherwise builtinClasses and classInfos should be consistent
		return nil
	}
	gc := &k8sv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: string(class),
		},
		Spec: k8sv1.GatewayClassSpec{
			ControllerName: k8sv1.GatewayController(classInfo.Controller),
			Description:    &classInfo.Description,
		},
	}
	_, err := c.classes.Create(gc)
	if err != nil && !kerrors.IsConflict(err) {
		return err
	} else if err != nil && kerrors.IsConflict(err) {
		// This is not really an error, just a race condition
		log.Infof("Attempted to create GatewayClass/%v, but it was already created", class)
	}
	if err != nil {
		return err
	}

	return nil
}

// Classes returns the kclient for GatewayClasses - useful for tests
func (c *ClassController) Classes() kclient.Client[*k8sv1.GatewayClass] {
	return c.classes
}
