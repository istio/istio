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
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	inferencev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/istio/pilot/pkg/config/kube/gateway/builtin"
	pilotcontrollers "istio.io/istio/pilot/pkg/controllers"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

// // ManagedLabel is the label used to identify resources managed by this controller
// const ManagedLabel = "inference.x-k8s.io/managed-by"

// ControllerName is the name of this controller for labeling resources it manages
const ControllerName = "inference-controller"

var supportedControllers = getSupportedControllers()

func getSupportedControllers() sets.Set[gatewayv1.GatewayController] {
	ret := sets.New[gatewayv1.GatewayController]()
	for _, controller := range builtin.BuiltinClasses {
		ret.Insert(controller)
	}
	return ret
}

type shadowServiceInfo struct {
	key      types.NamespacedName
	selector map[string]string
	poolName string
	poolUID  types.UID
	// targetPorts is the port number on the pods selected by the selector.
	// Currently, inference extension only supports a single target port.
	targetPorts []targetPort
}

type targetPort struct {
	port int32
}

type extRefInfo struct {
	name        string
	port        int32
	failureMode string
}

type InferencePool struct {
	shadowService  shadowServiceInfo
	extRef         extRefInfo
	gatewayParents sets.Set[types.NamespacedName] // Gateways that reference this InferencePool
}

func (i InferencePool) ResourceName() string {
	return i.shadowService.key.Namespace + "/" + i.shadowService.poolName
}

func InferencePoolCollection(
	pools krt.Collection[*inferencev1.InferencePool],
	services krt.Collection[*corev1.Service],
	httpRoutes krt.Collection[*gateway.HTTPRoute],
	gateways krt.Collection[*gateway.Gateway],
	routesByInferencePool krt.Index[string, *gateway.HTTPRoute],
	c *InferencePoolController,
	opts krt.OptionsBuilder,
) (krt.StatusCollection[*inferencev1.InferencePool, inferencev1.InferencePoolStatus], krt.Collection[InferencePool]) {
	return krt.NewStatusCollection(pools,
		func(
			ctx krt.HandlerContext,
			pool *inferencev1.InferencePool,
		) (*inferencev1.InferencePoolStatus, *InferencePool) {
			// Fetch HTTPRoutes that reference this InferencePool once and reuse
			routeList := krt.Fetch(ctx, httpRoutes, krt.FilterIndex(routesByInferencePool, pool.Namespace+"/"+pool.Name))

			// Find gateway parents that reference this InferencePool through HTTPRoutes
			gatewayParents := findGatewayParents(pool, routeList)

			// TODO: If no gateway parents, we should not do anything
			// 		note: we still need to filter out our Status to clean up previous reconciliations

			// Create the InferencePool only if there are Gateways connected
			var inferencePool *InferencePool
			if len(gatewayParents) > 0 {
				// Create the InferencePool object
				inferencePool = createInferencePoolObject(pool, gatewayParents)
			}

			// Calculate Status
			Status := calculateInferencePoolStatus(pool, gatewayParents, services, gateways, routeList)

			return Status, inferencePool
		}, opts.WithName("InferenceExtension")...)
}

// createInferencePoolObject creates the InferencePool object with shadow service and extension ref info
func createInferencePoolObject(pool *inferencev1.InferencePool, gatewayParents sets.Set[types.NamespacedName]) *InferencePool {
	// Build extension reference info
	extRef := extRefInfo{
		name: string(pool.Spec.EndpointPickerRef.Name),
	}

	if pool.Spec.EndpointPickerRef.Port == nil {
		log.Errorf("invalid InferencePool %s/%s; endpointPickerRef port is required", pool.Namespace, pool.Name)
		return nil
	}
	extRef.port = int32(pool.Spec.EndpointPickerRef.Port.Number)

	extRef.failureMode = string(inferencev1.EndpointPickerFailClose) // Default failure mode
	if pool.Spec.EndpointPickerRef.FailureMode != inferencev1.EndpointPickerFailClose {
		extRef.failureMode = string(pool.Spec.EndpointPickerRef.FailureMode)
	}

	svcName, err := model.InferencePoolServiceName(pool.Name)
	if err != nil {
		log.Errorf("failed to generate service name for InferencePool %s: %v", pool.Name, err)
		return nil
	}

	shadowSvcInfo := shadowServiceInfo{
		key: types.NamespacedName{
			Name:      svcName,
			Namespace: pool.GetNamespace(),
		},
		selector:    make(map[string]string, len(pool.Spec.Selector.MatchLabels)),
		poolName:    pool.GetName(),
		targetPorts: make([]targetPort, 0, len(pool.Spec.TargetPorts)),
		poolUID:     pool.GetUID(),
	}

	for k, v := range pool.Spec.Selector.MatchLabels {
		shadowSvcInfo.selector[string(k)] = string(v)
	}

	for _, port := range pool.Spec.TargetPorts {
		shadowSvcInfo.targetPorts = append(shadowSvcInfo.targetPorts, targetPort{port: int32(port.Number)})
	}

	return &InferencePool{
		shadowService:  shadowSvcInfo,
		extRef:         extRef,
		gatewayParents: gatewayParents,
	}
}

// calculateInferencePoolStatus calculates the complete Status for an InferencePool
func calculateInferencePoolStatus(
	pool *inferencev1.InferencePool,
	gatewayParents sets.Set[types.NamespacedName],
	services krt.Collection[*corev1.Service],
	gateways krt.Collection[*gateway.Gateway],
	routeList []*gateway.HTTPRoute,
) *inferencev1.InferencePoolStatus {
	// Calculate Status for each gateway parent
	existingParents := pool.Status.DeepCopy().Parents
	finalParents := []inferencev1.ParentStatus{}

	// Add existing parents from other controllers (not managed by us)
	for _, existingParent := range existingParents {
		gtwName := string(existingParent.ParentRef.Name)
		gtwNamespace := pool.Namespace
		if existingParent.ParentRef.Namespace != "" {
			gtwNamespace = string(existingParent.ParentRef.Namespace)
		}
		parentKey := types.NamespacedName{
			Name:      gtwName,
			Namespace: gtwNamespace,
		}

		isCurrentlyOurs := gatewayParents.Contains(parentKey)

		// Keep parents that are not ours and not default Status parents
		if !isCurrentlyOurs &&
			!isOurManagedGateway(gateways, gtwNamespace, gtwName) &&
			!isDefaultStatusParent(existingParent) {
			finalParents = append(finalParents, existingParent)
		}
	}

	// Calculate Status for each of our gateway parents
	for gatewayParent := range gatewayParents {
		parentStatus := calculateSingleParentStatus(pool, gatewayParent, services, existingParents, routeList)
		finalParents = append(finalParents, parentStatus)
	}

	return &inferencev1.InferencePoolStatus{
		Parents: finalParents,
	}
}

// findGatewayParents finds all Gateway parents that reference this InferencePool through HTTPRoutes
func findGatewayParents(
	pool *inferencev1.InferencePool,
	routeList []*gateway.HTTPRoute,
) sets.Set[types.NamespacedName] {
	gatewayParents := sets.New[types.NamespacedName]()

	for _, route := range routeList {
		// Only process routes that reference our InferencePool
		if !routeReferencesInferencePool(route, pool) {
			continue
		}

		// Check the route's parent Status to find accepted gateways
		for _, parentStatus := range route.Status.Parents {
			// Only consider parents managed by our supported controllers (from supportedControllers variable)
			// This filters out parents from other controllers we don't manage
			if !supportedControllers.Contains(parentStatus.ControllerName) {
				continue
			}

			// Get the gateway namespace (default to route namespace if not specified)
			gatewayNamespace := route.Namespace
			if ptr.OrEmpty(parentStatus.ParentRef.Namespace) != "" {
				gatewayNamespace = string(*parentStatus.ParentRef.Namespace)
			}

			gatewayParents.Insert(types.NamespacedName{
				Name:      string(parentStatus.ParentRef.Name),
				Namespace: gatewayNamespace,
			})
		}
	}

	return gatewayParents
}

// routeReferencesInferencePool checks if an HTTPRoute references the given InferencePool
func routeReferencesInferencePool(route *gateway.HTTPRoute, pool *inferencev1.InferencePool) bool {
	for _, rule := range route.Spec.Rules {
		for _, backendRef := range rule.BackendRefs {
			if !isInferencePoolBackendRef(backendRef.BackendRef) {
				continue
			}

			// Check if this backend ref points to our InferencePool
			if string(backendRef.BackendRef.Name) != pool.ObjectMeta.Name {
				continue
			}

			// Check namespace match
			backendRefNamespace := route.Namespace
			if ptr.OrEmpty(backendRef.BackendRef.Namespace) != "" {
				backendRefNamespace = string(*backendRef.BackendRef.Namespace)
			}

			if backendRefNamespace == pool.Namespace {
				return true
			}
		}
	}
	return false
}

// isInferencePoolBackendRef checks if a BackendRef is pointing to an InferencePool
func isInferencePoolBackendRef(backendRef gatewayv1.BackendRef) bool {
	return ptr.OrEmpty(backendRef.Group) == gatewayv1.Group(gvk.InferencePool.Group) &&
		ptr.OrEmpty(backendRef.Kind) == gatewayv1.Kind(gvk.InferencePool.Kind)
}

// calculateSingleParentStatus calculates the Status for a single gateway parent
func calculateSingleParentStatus(
	pool *inferencev1.InferencePool,
	gatewayParent types.NamespacedName,
	services krt.Collection[*corev1.Service],
	existingParents []inferencev1.ParentStatus,
	routeList []*gateway.HTTPRoute,
) inferencev1.ParentStatus {
	// Find existing Status for this parent to preserve some conditions
	var existingConditions []metav1.Condition
	for _, existingParent := range existingParents {
		if string(existingParent.ParentRef.Name) == gatewayParent.Name &&
			string(existingParent.ParentRef.Namespace) == gatewayParent.Namespace {
			existingConditions = existingParent.Conditions
			break
		}
	}

	// Filter to only keep conditions we manage
	filteredConditions := filterUsedConditions(existingConditions,
		inferencev1.InferencePoolConditionAccepted,
		inferencev1.InferencePoolConditionResolvedRefs)

	// Calculate Accepted Status by checking HTTPRoute parent Status
	acceptedStatus := calculateAcceptedStatus(pool, gatewayParent, routeList)

	// Calculate ResolvedRefs Status
	resolvedRefsStatus := calculateResolvedRefsStatus(pool, services)

	// Build the final Status
	return inferencev1.ParentStatus{
		ParentRef: inferencev1.ParentReference{
			Group:     (*inferencev1.Group)(&gvk.Gateway.Group),
			Kind:      inferencev1.Kind(gvk.Gateway.Kind),
			Namespace: inferencev1.Namespace(gatewayParent.Namespace),
			Name:      inferencev1.ObjectName(gatewayParent.Name),
		},
		Conditions: pilotcontrollers.SetConditions(pool.Generation, filteredConditions, map[string]*pilotcontrollers.Condition{
			string(inferencev1.InferencePoolConditionAccepted):     acceptedStatus,
			string(inferencev1.InferencePoolConditionResolvedRefs): resolvedRefsStatus,
		}),
	}
}

// calculateAcceptedStatus determines if the InferencePool is accepted by checking HTTPRoute parent Status
func calculateAcceptedStatus(
	pool *inferencev1.InferencePool,
	gatewayParent types.NamespacedName,
	routeList []*gateway.HTTPRoute,
) *pilotcontrollers.Condition {
	// Check if any HTTPRoute references this InferencePool and has this gateway as an accepted parent
	for _, route := range routeList {
		// Only process routes that reference our InferencePool
		if !routeReferencesInferencePool(route, pool) {
			continue
		}

		// Check if this route has our gateway as a parent and if it's accepted
		for _, parentStatus := range route.Status.Parents {
			// Only consider parents managed by supported controllers
			if !supportedControllers.Contains(parentStatus.ControllerName) {
				continue
			}

			// Check if this parent refers to our gateway
			gatewayNamespace := route.Namespace
			if ptr.OrEmpty(parentStatus.ParentRef.Namespace) != "" {
				gatewayNamespace = string(*parentStatus.ParentRef.Namespace)
			}

			if string(parentStatus.ParentRef.Name) == gatewayParent.Name && gatewayNamespace == gatewayParent.Namespace {
				// Check if this parent is accepted
				for _, parentCondition := range parentStatus.Conditions {
					if parentCondition.Type == string(gatewayv1.RouteConditionAccepted) {
						if parentCondition.Status == metav1.ConditionTrue {
							return &pilotcontrollers.Condition{
								Reason:  string(inferencev1.InferencePoolReasonAccepted),
								Status:  metav1.ConditionTrue,
								Message: "Referenced by an HTTPRoute accepted by the parentRef Gateway",
							}
						}
						return &pilotcontrollers.Condition{
							Reason: string(inferencev1.InferencePoolReasonHTTPRouteNotAccepted),
							Status: metav1.ConditionFalse,
							Message: fmt.Sprintf("Referenced HTTPRoute %s/%s not accepted by Gateway %s/%s: %s",
								route.Namespace, route.Name, gatewayParent.Namespace, gatewayParent.Name, parentCondition.Message),
						}
					}
				}

				// If no Accepted condition found, treat as unknown (parent is listed in Status)
				return &pilotcontrollers.Condition{
					Reason:  string(inferencev1.InferencePoolReasonAccepted),
					Status:  metav1.ConditionUnknown,
					Message: "Referenced by an HTTPRoute unknown parentRef Gateway status",
				}
			}
		}
	}

	// If we get here, no HTTPRoute was found that references this InferencePool with this gateway as parent
	// This shouldn't happen in normal operation since we only call this for known gateway parents
	return &pilotcontrollers.Condition{
		Reason: string(inferencev1.InferencePoolReasonHTTPRouteNotAccepted),
		Status: metav1.ConditionFalse,
		Message: fmt.Sprintf("No HTTPRoute found referencing this InferencePool with Gateway %s/%s as parent",
			gatewayParent.Namespace, gatewayParent.Name),
	}
}

// calculateResolvedRefsStatus determines the states of the ExtensionRef
// * if the kind is supported
// * if the extensionRef is defined
// * if the service exists in the same namespace as the InferencePool
func calculateResolvedRefsStatus(
	pool *inferencev1.InferencePool,
	services krt.Collection[*corev1.Service],
) *pilotcontrollers.Condition {
	// Default Kind to Service if unset
	kind := string(pool.Spec.EndpointPickerRef.Kind)
	if kind == "" {
		kind = gvk.Service.Kind
	}

	if kind != gvk.Service.Kind {
		return &pilotcontrollers.Condition{
			Reason:  string(inferencev1.InferencePoolReasonInvalidExtensionRef),
			Status:  metav1.ConditionFalse,
			Message: "Unsupported ExtensionRef kind " + kind,
		}
	}

	name := string(pool.Spec.EndpointPickerRef.Name)
	if name == "" {
		return &pilotcontrollers.Condition{
			Reason:  string(inferencev1.InferencePoolReasonInvalidExtensionRef),
			Status:  metav1.ConditionFalse,
			Message: "ExtensionRef not defined",
		}
	}

	svc := ptr.Flatten(services.GetKey(fmt.Sprintf("%s/%s", pool.Namespace, name)))
	if svc == nil {
		return &pilotcontrollers.Condition{
			Reason:  string(inferencev1.InferencePoolReasonInvalidExtensionRef),
			Status:  metav1.ConditionFalse,
			Message: "Referenced ExtensionRef not found " + name,
		}
	}

	return &pilotcontrollers.Condition{
		Reason:  string(inferencev1.InferencePoolReasonResolvedRefs),
		Status:  metav1.ConditionTrue,
		Message: "Referenced ExtensionRef resolved successfully",
	}
}

// isDefaultStatusParent checks if this is a default Status parent entry
func isDefaultStatusParent(parent inferencev1.ParentStatus) bool {
	return string(parent.ParentRef.Kind) == "Status" && parent.ParentRef.Name == "default"
}

// isOurManagedGateway checks if a Gateway is managed by one of our supported controllers
// This is used to identify stale parent entries that we previously added but are no longer referenced by HTTPRoutes
func isOurManagedGateway(gateways krt.Collection[*gateway.Gateway], namespace, name string) bool {
	gtw := ptr.Flatten(gateways.GetKey(fmt.Sprintf("%s/%s", namespace, name)))
	if gtw == nil {
		return false
	}
	_, ok := builtin.BuiltinClasses[gtw.Spec.GatewayClassName]
	return ok
}

func filterUsedConditions(conditions []metav1.Condition, usedConditions ...inferencev1.InferencePoolConditionType) []metav1.Condition {
	var result []metav1.Condition
	for _, condition := range conditions {
		if slices.Contains(usedConditions, inferencev1.InferencePoolConditionType(condition.Type)) {
			result = append(result, condition)
		}
	}
	return result
}

func translateShadowServiceToService(existingLabels map[string]string, shadow shadowServiceInfo, extRef extRefInfo) *corev1.Service {
	// Create the ports used by the shadow service
	ports := make([]corev1.ServicePort, 0, len(shadow.targetPorts))
	dummyPort := int32(54321) // Dummy port, not used for anything
	for i, port := range shadow.targetPorts {
		ports = append(ports, corev1.ServicePort{
			Name:       "port" + strconv.Itoa(i),
			Protocol:   corev1.ProtocolTCP,
			Port:       dummyPort + int32(i),
			TargetPort: intstr.FromInt(int(port.port)),
		})
	}

	// Create a new service object based on the shadow service info
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      shadow.key.Name,
			Namespace: shadow.key.Namespace,
			Labels: maps.MergeCopy(map[string]string{
				model.InferencePoolRefLabel:                shadow.poolName,
				model.InferencePoolExtensionRefSvc:         extRef.name,
				model.InferencePoolExtensionRefPort:        strconv.Itoa(int(extRef.port)),
				model.InferencePoolExtensionRefFailureMode: extRef.failureMode,
				constants.InternalServiceSemantics:         constants.ServiceSemanticsInferencePool,
			}, existingLabels),
		},
		Spec: corev1.ServiceSpec{
			Selector:  shadow.selector,
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: corev1.ClusterIPNone, // Headless service
			Ports:     ports,
		},
	}

	svc.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: gvk.InferencePool.GroupVersion(),
			Kind:       gvk.InferencePool.Kind,
			Name:       shadow.poolName,
			UID:        shadow.poolUID,
		},
	})

	return svc
}

func (c *InferencePoolController) reconcileShadowService(
	svcClient kclient.Client[*corev1.Service],
	inferencePools krt.Collection[InferencePool],
	servicesCollection krt.Collection[*corev1.Service],
) func(key types.NamespacedName) error {
	return func(key types.NamespacedName) error {
		// Find the InferencePool that matches the key
		pool := inferencePools.GetKey(key.String())
		if pool == nil {
			// we'll generally ignore these scenarios, since the InferencePool may have been deleted
			log.Debugf("inferencepool no longer exists", key.String())
			return nil
		}

		// We found the InferencePool, now we need to translate it to a shadow Service
		// and check if it exists already
		existingService := ptr.Flatten(servicesCollection.GetKey(pool.shadowService.key.String()))

		// Check if we can manage this service
		var existingLabels map[string]string
		if existingService != nil {
			existingLabels = existingService.GetLabels()
			canManage, _ := c.canManageShadowServiceForInference(existingService)
			if !canManage {
				log.Debugf("skipping service %s/%s, already managed by another controller", key.Namespace, key.Name)
				return nil
			}
		}

		service := translateShadowServiceToService(existingLabels, pool.shadowService, pool.extRef)

		var err error
		if existingService == nil {
			// Create the service if it doesn't exist
			_, err = svcClient.Create(service)
		} else {
			// TODO: Don't overwrite resources: https://github.com/istio/istio/issues/56667
			service.ResourceVersion = existingService.ResourceVersion
			_, err = svcClient.Update(service)
		}

		return err
	}
}

// canManage checks if a service should be managed by this controller
func (c *InferencePoolController) canManageShadowServiceForInference(obj *corev1.Service) (bool, string) {
	if obj == nil {
		// No object exists, we can manage it
		return true, ""
	}

	_, inferencePoolManaged := obj.GetLabels()[model.InferencePoolRefLabel]
	// We can manage if it has no manager or ifmodel. we are the manager
	return inferencePoolManaged, obj.GetResourceVersion()
}

func indexHTTPRouteByInferencePool(o *gateway.HTTPRoute) []string {
	var keys []string
	for _, rule := range o.Spec.Rules {
		for _, backendRef := range rule.BackendRefs {
			if isInferencePoolBackendRef(backendRef.BackendRef) {
				// If BackendRef.Namespace is not specified, the backend is in the same namespace as the HTTPRoute's
				backendRefNamespace := o.Namespace
				if ptr.OrEmpty(backendRef.BackendRef.Namespace) != "" {
					backendRefNamespace = string(*backendRef.BackendRef.Namespace)
				}
				key := backendRefNamespace + "/" + string(backendRef.Name)
				keys = append(keys, key)
			}
		}
	}
	return keys
}
