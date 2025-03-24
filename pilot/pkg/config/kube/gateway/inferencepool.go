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
	"crypto/sha256"
	"fmt"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	inferencev1alpha2 "sigs.k8s.io/gateway-api-inference-extension/api/v1alpha2"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
)

const (
	maxServiceNameLength          = 63
	hashSize                      = 8
	InferencePoolRefLabel         = "inference.x-k8s.io/inference-pool-name"
	InferencePoolExtensionRefSvc  = "inference.x-k8s.io/extension-service"
	InferencePoolExtensionRefPort = "inference.x-k8s.io/extension-port"
)

// // ManagedLabel is the label used to identify resources managed by this controller
// const ManagedLabel = "inference.x-k8s.io/managed-by"

// ControllerName is the name of this controller for labeling resources it manages
const ControllerName = "inference-controller"

var supportedControllers = getSupportedControllers()

func getSupportedControllers() sets.Set[gatewayv1.GatewayController] {
	ret := sets.New[gatewayv1.GatewayController]()
	for _, controller := range builtinClasses {
		ret.Insert(controller)
	}
	return ret
}

// InferencePoolController implements a controller that materializes an InferencePool
// into a Kubernetes Service to allow traffic to the inference pool.
type InferencePoolController struct {
	client     kube.Client
	queue      controllers.Queue
	pools      kclient.Client[*inferencev1alpha2.InferencePool]
	services   kclient.Client[*corev1.Service]
	httpRoutes kclient.Client[*gateway.HTTPRoute]
	gateways   kclient.Client[*gateway.Gateway]
	clients    map[schema.GroupVersionResource]getter

	// ServiceToEndpointPickerService map[types.NamespacedName]*corev1.Service
}

// NewInferencePoolController constructs a new InferencePoolController and registers required informers.
func NewInferencePoolController(client kube.Client) *InferencePoolController {
	filter := kclient.Filter{ObjectFilter: client.ObjectFilter()}
	pools := kclient.NewFiltered[*inferencev1alpha2.InferencePool](client, filter)

	ic := &InferencePoolController{
		client:  client,
		clients: map[schema.GroupVersionResource]getter{},
		pools:   pools,
	}

	ic.queue = controllers.NewQueue("inference pool",
		controllers.WithReconciler(ic.Reconcile),
		controllers.WithMaxAttempts(5))

	// Set up a handler that will add the parent InferencePool object onto the queue
	parentHandler := controllers.ObjectHandler(controllers.EnqueueForParentHandler(ic.queue, gvk.InferencePool))

	ic.services = kclient.NewFiltered[*corev1.Service](client, filter)
	ic.services.AddEventHandler(parentHandler)
	ic.clients[gvr.Service] = NewUntypedWrapper(ic.services)

	ic.httpRoutes = kclient.NewFiltered[*gateway.HTTPRoute](client, filter)
	// Watch for changes in HTTPRoutes and enqueue relevant InferencePools
	ic.httpRoutes.AddEventHandler(controllers.ObjectHandler(func(obj controllers.Object) {
		route := obj.(*gateway.HTTPRoute)
		for _, pool := range ic.pools.List(route.Namespace, labels.Everything()) {
			for _, rule := range route.Spec.Rules {
				for _, httpBackendRef := range rule.BackendRefs {
					if httpBackendRef.BackendRef.Group == nil || httpBackendRef.BackendRef.Kind == nil {
						continue
					}
					if string(*httpBackendRef.BackendRef.Group) == gvk.InferencePool.Group &&
						string(*httpBackendRef.BackendRef.Kind) == gvk.InferencePool.Kind &&
						string(httpBackendRef.BackendRef.Name) == pool.ObjectMeta.Name {
						ic.queue.Add(types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace})
					}
				}
			}
		}
	}))
	ic.clients[gvr.HTTPRoute] = NewUntypedWrapper(ic.httpRoutes)

	// Watch for changes in Gateways and enqueue relevant InferencePools
	ic.gateways = kclient.NewFiltered[*gateway.Gateway](client, filter)
	ic.gateways.AddEventHandler(controllers.ObjectHandler(func(obj controllers.Object) {
		gateway := obj.(*gateway.Gateway)
		// TODO(liorlieberman): can we restrict to gateway.namespace or we support cross ns?
		for _, pool := range ic.pools.List(metav1.NamespaceAll, labels.Everything()) {
			for _, parent := range pool.Status.Parents {
				if parent.GatewayRef.Name == gateway.Name && parent.GatewayRef.Namespace == gateway.Namespace {
					ic.queue.Add(types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace})
				}
			}
		}
	}))
	ic.clients[gvr.KubernetesGateway] = NewUntypedWrapper(ic.gateways)

	pools.AddEventHandler(controllers.ObjectHandler(ic.queue.AddObject))

	return ic
}

// Run starts the controller
func (ic *InferencePoolController) Run(stop <-chan struct{}) {
	kube.WaitForCacheSync(
		"inference pool controller",
		stop,
		ic.pools.HasSynced,
		ic.services.HasSynced,
		ic.gateways.HasSynced,
		ic.httpRoutes.HasSynced,
	)
	ic.queue.Run(stop)
	controllers.ShutdownAll(ic.pools, ic.services, ic.gateways, ic.httpRoutes)
}

// Reconcile takes the name of an InferencePool and ensures the cluster is in the desired state
func (ic *InferencePoolController) Reconcile(req types.NamespacedName) error {
	log := log.WithLabels("inferencepool", req)

	pool := ic.pools.Get(req.Name, req.Namespace)
	if pool == nil {
		log.Debugf("inference pool no longer exists")
		// TODO
		return nil
	}

	// TODO: check if this inferencepool is managed by the controller.

	return ic.configureInferencePool(log, pool)
}

// // isManaged checks if the controller should manage this InferencePool
// func isManaged(pool *inferencev1alpha2.InferencePool) bool {
// 	return true
// }

// configureInferencePool handles the reconciliation of the InferencePool resource
func (ic *InferencePoolController) configureInferencePool(log *istiolog.Scope, pool *inferencev1alpha2.InferencePool) error {
	log.Info("reconciling inference pool")

	service, err := translateInferencePoolToService(pool)
	if err != nil {
		return fmt.Errorf("failed to translate InferencePool to Service: %v", err)
	}

	// Add management labels/annotations
	ensureLabels(service, pool.Name)

	// Apply the service

	err = ic.reconcileShadowService(service)
	if err != nil {
		return fmt.Errorf("failed to apply Service: %v", err)
	}

	gatewayParentsToEnsure := sets.New[types.NamespacedName]()
	routeList := ic.httpRoutes.List(pool.Namespace, labels.Everything())
	for _, r := range routeList {
		for _, rule := range r.Spec.Rules {
			for _, httpBackendRef := range rule.BackendRefs {
				if httpBackendRef.BackendRef.Group == nil || httpBackendRef.BackendRef.Kind == nil {
					continue
				}
				if string(*httpBackendRef.BackendRef.Group) == gvk.InferencePool.Group &&
					string(*httpBackendRef.BackendRef.Kind) == gvk.InferencePool.Kind &&
					string(httpBackendRef.BackendRef.Name) == pool.ObjectMeta.Name {
					for _, p := range r.Status.Parents {
						if supportedControllers.Contains(p.ControllerName) {
							ns := r.Namespace
							if p.ParentRef.Namespace != nil && *p.ParentRef.Namespace != "" {
								ns = string(*p.ParentRef.Namespace)
							}
							gatewayParentsToEnsure.Insert(types.NamespacedName{Name: string(p.ParentRef.Name), Namespace: ns})
						}
					}
				}
			}
		}
	}
	existingParents := pool.Status.Parents
	newParents := []*inferencev1alpha2.PoolStatus{}
	for gtw := range gatewayParentsToEnsure {
		newParents = append(newParents, poolStatusTmpl(gtw.Name, gtw.Namespace, pool.Generation))
	}

	finalParents := []inferencev1alpha2.PoolStatus{}
	finalParentsGatewaySet := sets.New[types.NamespacedName]()
	for _, existingParent := range existingParents {
		gwKey := types.NamespacedName{Name: existingParent.GatewayRef.Name, Namespace: existingParent.GatewayRef.Namespace}
		if !ic.isManagedGateway(existingParent) || gatewayParentsToEnsure.Contains(gwKey) {
			finalParents = append(finalParents, existingParent)
			finalParentsGatewaySet.Insert(gwKey)
		}
	}
	for _, newParent := range newParents {
		gwKey := types.NamespacedName{Name: newParent.GatewayRef.Name, Namespace: newParent.GatewayRef.Namespace}
		if !finalParentsGatewaySet.Contains(gwKey) {
			finalParents = append(finalParents, *newParent)
			finalParentsGatewaySet.Insert(gwKey)
		}
	}

	if !parentsEqual(finalParents, pool.Status.Parents) {
		pool.Status.Parents = finalParents
		_, err := ic.pools.UpdateStatus(pool)
		if err != nil {
			return fmt.Errorf("error updating inferencepool status: %v", err)
		}
	}
	log.Info("inference pool service updated")
	return nil
}

func parentsEqual(a, b []inferencev1alpha2.PoolStatus) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].GatewayRef.Name != b[i].GatewayRef.Name || a[i].GatewayRef.Namespace != b[i].GatewayRef.Namespace {
			return false
		}
	}
	return true
}

// isManagedGateway checks if the Gateway is controlled by this controller
func (ic *InferencePoolController) isManagedGateway(parent inferencev1alpha2.PoolStatus) bool {
	gtw := ic.gateways.Get(parent.GatewayRef.Name, parent.GatewayRef.Namespace)
	if gtw == nil {
		return false
	}
	_, ok := builtinClasses[gtw.Spec.GatewayClassName]
	return ok
}

func poolStatusTmpl(gwName, ns string, generation int64) *inferencev1alpha2.PoolStatus {
	return &inferencev1alpha2.PoolStatus{
		GatewayRef: corev1.ObjectReference{
			APIVersion: gatewayv1.GroupVersion.String(),
			Kind:       gvk.Gateway.Kind,
			Namespace:  ns,
			Name:       gwName,
		},
		Conditions: []metav1.Condition{
			{
				Type:               string(inferencev1alpha2.InferencePoolConditionAccepted),
				Status:             metav1.ConditionTrue,
				Reason:             string(inferencev1alpha2.InferencePoolReasonAccepted),
				Message:            "Referenced by an HTTPRoute accepted by the parentRef Gateway",
				ObservedGeneration: generation,
				// TODO(liorlieberman): verify that this is good.
				LastTransitionTime: metav1.NewTime(time.Now()),
			},
		},
	}
}

// generateHash generates an 8-character SHA256 hash of the input string.
func generateHash(input string, length int) string {
	hashBytes := sha256.Sum256([]byte(input))
	hashString := fmt.Sprintf("%x", hashBytes) // Convert to hexadecimal string
	return hashString[:length]                 // Truncate to desired length
}

func InferencePoolServiceName(poolName string) (string, error) {
	ipSeparator := "-ip-"
	hash := generateHash(poolName, hashSize)
	svcName := poolName + ipSeparator + hash
	// Truncate if necessary to meet the Kubernetes naming constraints
	if len(svcName) > maxServiceNameLength {
		// Calculate the maximum allowed base name length
		maxBaseLength := maxServiceNameLength - len(ipSeparator) - hashSize
		if maxBaseLength < 0 {
			return "", fmt.Errorf("inference pool name: %s is too long", poolName)
		}

		// Truncate the base name and reconstruct the service name
		truncatedBase := poolName[:maxBaseLength]
		svcName = truncatedBase + ipSeparator + hash
	}
	return svcName, nil
}

// translateInferencePoolToService converts an InferencePool to a Service
func translateInferencePoolToService(pool *inferencev1alpha2.InferencePool) (*corev1.Service, error) {
	svcName, err := InferencePoolServiceName(pool.Name)
	if err != nil {
		return nil, err
	}

	shadowSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: pool.GetNamespace(),
		},
	}
	if shadowSvc.Labels == nil {
		shadowSvc.Labels = map[string]string{}
	}
	shadowSvc.Labels[InferencePoolRefLabel] = pool.GetName()
	// TODO: for now, supporting only service
	extRef := pool.Spec.ExtensionRef
	shadowSvc.Labels[InferencePoolExtensionRefSvc] = string(extRef.Name)
	if extRef.PortNumber != nil {
		shadowSvc.Labels[InferencePoolExtensionRefPort] = strconv.Itoa(int(*extRef.PortNumber))
	} else {
		shadowSvc.Labels[InferencePoolExtensionRefPort] = "9002"
	}
	shadowSvc.Labels[constants.InternalServiceSemantics] = constants.ServiceSemanticsInferencePool
	// adding dummy port, not used for anything
	shadowSvc.Spec.Ports = []corev1.ServicePort{
		{
			Protocol:   "TCP",
			Port:       int32(54321),
			TargetPort: intstr.FromInt(int(pool.Spec.TargetPortNumber)),
		},
	}
	if shadowSvc.Spec.Selector == nil {
		shadowSvc.Spec.Selector = map[string]string{}
	}
	for k, v := range pool.Spec.Selector {
		shadowSvc.Spec.Selector[string(k)] = string(v)
	}
	shadowSvc.Spec.ClusterIP = corev1.ClusterIPNone
	shadowSvc.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: gvk.InferencePool.GroupVersion(),
			Kind:       gvk.InferencePool.Kind,
			Name:       pool.GetName(),
			UID:        pool.GetUID(),
		},
	})
	return shadowSvc, nil
}

// ensureLabels adds management labels to resources
func ensureLabels(obj metav1.Object, poolName string) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[InferencePoolRefLabel] = poolName
	obj.SetLabels(labels)
}

func (ic *InferencePoolController) reconcileShadowService(obj interface{}) error {
	if service, ok := obj.(*corev1.Service); ok {
		name := service.Name
		namespace := service.Namespace
		// Get the existing service if it exists
		existingService := ic.services.Get(service.Name, service.Namespace)

		// Check if we can manage this service
		if existingService != nil {
			canManage, _ := ic.canManage(existingService)
			if !canManage {
				log.Debugf("skipping service %s/%s, already managed by another controller", namespace, name)
				return nil
			}
		}

		var err error
		if existingService == nil {
			// Create the service if it doesn't exist
			_, err = ic.client.Kube().CoreV1().Services(service.Namespace).Create(
				context.Background(), service, metav1.CreateOptions{})
		} else {
			// Update the service if it exists
			// Note: We need to use Update instead of Patch for simpler implementation
			// This means we might overwrite other fields, but that's the expected behavior
			// for our controller
			service.ResourceVersion = existingService.ResourceVersion
			_, err = ic.client.Kube().CoreV1().Services(service.Namespace).Update(
				context.Background(), service, metav1.UpdateOptions{})
		}

		if err != nil {
			return fmt.Errorf("failed to apply service %s/%s: %v", service.Namespace, service.Name, err)
		}
		return nil
	}
	return nil
}

// canManage checks if a service should be managed by this controller
func (ic *InferencePoolController) canManage(obj *corev1.Service) (bool, string) {
	if obj == nil {
		// No object exists, we can manage it
		return true, ""
	}

	_, inferencePoolManaged := obj.GetLabels()[InferencePoolRefLabel]
	// We can manage if it has no manager or if we are the manager
	return inferencePoolManaged, obj.GetResourceVersion()
}
