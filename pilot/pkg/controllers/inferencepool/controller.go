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

package inferencepool

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	inferencev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"

	pilotcontrollers "istio.io/istio/pilot/pkg/controllers"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/revisions"
)

type inferencePoolController struct {
	client       kube.Client
	cluster      cluster.ID
	revision     string
	status       *status.StatusCollections
	tagWatcher   krt.RecomputeProtected[revisions.TagWatcher]
	waitForCRD   func(class schema.GroupVersionResource, stop <-chan struct{}) bool
	stop         chan struct{}
	handlers     []krt.HandlerRegistration
	domainSuffix string

	shadowServiceReconciler controllers.Queue
}

type Inputs struct {
	services krt.Collection[*corev1.Service]

	gateways       krt.Collection[*gateway.Gateway]
	httpRoutes     krt.Collection[*gateway.HTTPRoute]
	inferencePools krt.Collection[*inferencev1.InferencePool]
}

func NewController(
	kc kube.Client,
	waitForCRD func(class schema.GroupVersionResource, stop <-chan struct{}) bool,
	options controller.Options,
) *inferencePoolController {
	stop := make(chan struct{})
	opts := krt.NewOptionsBuilder(stop, "gateway", options.KrtDebugger)

	tw := revisions.NewTagWatcher(kc, options.Revision, options.SystemNamespace)
	ipc := &inferencePoolController{
		client:       kc,
		cluster:      options.ClusterID,
		revision:     options.Revision,
		status:       &status.StatusCollections{},
		tagWatcher:   krt.NewRecomputeProtected(tw, false, opts.WithName("tagWatcher")...),
		waitForCRD:   waitForCRD,
		stop:         stop,
		domainSuffix: options.DomainSuffix,
		handlers:     []krt.HandlerRegistration{},
	}

	svcClient := kclient.NewFiltered[*corev1.Service](kc, kubetypes.Filter{ObjectFilter: kc.ObjectFilter()})
	inputs := Inputs{
		services:       krt.WrapClient(svcClient, opts.WithName("informer/Services")...),
		gateways:       krt.NewInformer[*gateway.Gateway](kc, opts.WithName("informer/Gateways")...),
		httpRoutes:     krt.NewInformer[*gateway.HTTPRoute](kc, opts.WithName("informer/HTTPRoutes")...),
		inferencePools: krt.NewInformer[*inferencev1.InferencePool](kc, opts.WithName("informer/InferencePools")...),
	}

	httpRoutesByInferencePool := krt.NewIndex(inputs.httpRoutes, "inferencepool-route", indexHTTPRouteByInferencePool)

	InferencePoolStatus, InferencePools := InferencePoolCollection(
		inputs.inferencePools,
		inputs.services,
		inputs.httpRoutes,
		inputs.gateways,
		httpRoutesByInferencePool,
		ipc,
		opts,
	)

	// Create a queue for handling service updates.
	// We create the queue even if the env var is off just to prevent nil pointer issues.
	ipc.shadowServiceReconciler = controllers.NewQueue("inference pool shadow service reconciler",
		controllers.WithReconciler(ipc.reconcileShadowService(svcClient, InferencePools, inputs.services)),
		controllers.WithMaxAttempts(5))

	status.RegisterStatus(ipc.status, InferencePoolStatus, pilotcontrollers.GetStatus)

	ipc.handlers = append(ipc.handlers,
		InferencePools.Register(func(e krt.Event[InferencePool]) {
			obj := e.Latest()
			ipc.shadowServiceReconciler.Add(types.NamespacedName{
				Namespace: obj.shadowService.key.Namespace,
				Name:      obj.shadowService.poolName,
			})
		}),
		// Reconcile shadow services if users break them.
		inputs.services.Register(func(o krt.Event[*corev1.Service]) {
			obj := o.Latest()
			// We only care about services that are tagged with the internal service semantics label.
			if obj.GetLabels()[constants.InternalServiceSemantics] != constants.ServiceSemanticsInferencePool {
				return
			}
			// We only care about delete events
			if o.Event != controllers.EventDelete && o.Event != controllers.EventUpdate {
				return
			}

			poolName, ok := obj.Labels[model.InferencePoolRefLabel]
			if !ok && o.Event == controllers.EventUpdate && o.Old != nil {
				// Try and find the label from the old object
				old := ptr.Flatten(o.Old)
				poolName, ok = old.Labels[model.InferencePoolRefLabel]
			}

			if !ok {
				log.Errorf("service %s/%s is missing the %s label, cannot reconcile shadow service",
					obj.Namespace, obj.Name, model.InferencePoolRefLabel)
				return
			}

			// Add it back
			ipc.shadowServiceReconciler.Add(types.NamespacedName{
				Namespace: obj.Namespace,
				Name:      poolName,
			})
			log.Infof("Re-adding shadow service for deleted inference pool service %s/%s",
				obj.Namespace, obj.Name)
		}),
	)
	return ipc
}

func (c *inferencePoolController) SetStatusQueue(t *testing.T, queue status.Queue) []krt.Syncer {
	return c.status.SetQueue(queue)
}

func (c *inferencePoolController) Run(stop <-chan struct{}) {
	tw := c.tagWatcher.AccessUnprotected()
	go tw.Run(stop)
	go c.shadowServiceReconciler.Run(stop)
	go func() {
		kube.WaitForCacheSync("inferencepool tag watcher", stop, tw.HasSynced)
		c.tagWatcher.MarkSynced()
	}()

	<-stop
	close(c.stop)
}

func (c *inferencePoolController) HasSynced() bool {
	for _, h := range c.handlers {
		if !h.HasSynced() {
			return false
		}
	}
	return true
}

// buildClient is a small wrapper to build a krt collection based on a delayed informer.
func buildClient[I controllers.ComparableObject](
	c *inferencePoolController,
	kc kube.Client,
	res schema.GroupVersionResource,
	opts krt.OptionsBuilder,
	name string,
) krt.Collection[I] {
	filter := kclient.Filter{
		ObjectFilter: kubetypes.ComposeFilters(kc.ObjectFilter(), pilotcontrollers.InRevision(c.revision)),
	}

	// all other types are filtered by revision, but for gateways we need to select tags as well
	if res == gvr.KubernetesGateway {
		filter.ObjectFilter = kc.ObjectFilter()
	}

	cc := kclient.NewDelayedInformer[I](kc, res, kubetypes.StandardInformer, filter)
	return krt.WrapClient[I](cc, opts.WithName(name)...)
}
