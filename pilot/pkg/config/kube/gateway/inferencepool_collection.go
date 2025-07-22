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
	"crypto/sha256"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	inferencev1alpha2 "sigs.k8s.io/gateway-api-inference-extension/api/v1alpha2"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

const (
	maxServiceNameLength                 = 63
	hashSize                             = 8
	InferencePoolRefLabel                = "istio.io/inferencepool-name"
	InferencePoolExtensionRefSvc         = "istio.io/inferencepool-extension-service"
	InferencePoolExtensionRefPort        = "istio.io/inferencepool-extension-port"
	InferencePoolExtensionRefFailureMode = "istio.io/inferencepool-extension-failure-mode"
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

type shadowServiceInfo struct {
	key        types.NamespacedName
	selector   map[string]string
	poolName   string
	poolUID    types.UID
	targetPort int32
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
	pools krt.Collection[*inferencev1alpha2.InferencePool],
	services krt.Collection[*corev1.Service],
	httpRoutes krt.Collection[*gateway.HTTPRoute],
	gateways krt.Collection[*gateway.Gateway],
	routesByInferencePool krt.Index[string, *gateway.HTTPRoute],
	c *Controller,
	opts krt.OptionsBuilder,
) (krt.StatusCollection[*inferencev1alpha2.InferencePool, inferencev1alpha2.InferencePoolStatus], krt.Collection[InferencePool]) {
	return krt.NewStatusCollection(pools,
		func(
			ctx krt.HandlerContext,
			pool *inferencev1alpha2.InferencePool,
		) (*inferencev1alpha2.InferencePoolStatus, *InferencePool) {
			// Fetch HTTPRoutes that reference this InferencePool once and reuse
			routeList := krt.Fetch(ctx, httpRoutes, krt.FilterIndex(routesByInferencePool, pool.Namespace+"/"+pool.Name))

			// Find gateway parents that reference this InferencePool through HTTPRoutes
			gatewayParents := findGatewayParents(pool, routeList)

			// TODO: If no gateway parents, we should not do anything
			// 		note: we stil need to filter out our Status to clean up previous reconciliations

			// Create the InferencePool only if there are Gateways connected
			var inferencePool *InferencePool
			if len(gatewayParents) > 0 {
				// Create the InferencePool object
				inferencePool = createInferencePoolObject(pool, gatewayParents)
			}

			// Calculate status
			status := calculateInferencePoolStatus(pool, gatewayParents, services, gateways, routeList)

			return status, inferencePool
		}, opts.WithName("InferenceExtension")...)
}

// createInferencePoolObject creates the InferencePool object with shadow service and extension ref info
func createInferencePoolObject(pool *inferencev1alpha2.InferencePool, gatewayParents sets.Set[types.NamespacedName]) *InferencePool {
	// Build extension reference info
	extRef := extRefInfo{
		name: string(pool.Spec.ExtensionRef.Name),
	}
	if pool.Spec.ExtensionRef.PortNumber != nil {
		extRef.port = int32(*pool.Spec.ExtensionRef.PortNumber)
	} else {
		extRef.port = 9002 // Default port for the inference extension
	}
	if pool.Spec.ExtensionRef.FailureMode != nil {
		extRef.failureMode = string(*pool.Spec.ExtensionRef.FailureMode)
	} else {
		extRef.failureMode = string(inferencev1alpha2.FailClose)
	}

	svcName, err := InferencePoolServiceName(pool.Name)
	if err != nil {
		log.Errorf("failed to generate service name for InferencePool %s: %v", pool.Name, err)
		return nil
	}

	shadowSvcInfo := shadowServiceInfo{
		key: types.NamespacedName{
			Name:      svcName,
			Namespace: pool.GetNamespace(),
		},
		selector:   make(map[string]string, len(pool.Spec.Selector)),
		poolName:   pool.GetName(),
		targetPort: pool.Spec.TargetPortNumber,
		poolUID:    pool.GetUID(),
	}

	for k, v := range pool.Spec.Selector {
		shadowSvcInfo.selector[string(k)] = string(v)
	}

	return &InferencePool{
		shadowService:  shadowSvcInfo,
		extRef:         extRef,
		gatewayParents: gatewayParents,
	}
}

// calculateInferencePoolStatus calculates the complete status for an InferencePool
func calculateInferencePoolStatus(
	pool *inferencev1alpha2.InferencePool,
	gatewayParents sets.Set[types.NamespacedName],
	services krt.Collection[*corev1.Service],
	gateways krt.Collection[*gateway.Gateway],
	routeList []*gateway.HTTPRoute,
) *inferencev1alpha2.InferencePoolStatus {
	// Calculate status for each gateway parent
	existingParents := pool.Status.DeepCopy().Parents
	finalParents := []inferencev1alpha2.PoolStatus{}

	// Add existing parents from other controllers (not managed by us)
	for _, existingParent := range existingParents {
		parentKey := types.NamespacedName{
			Name:      existingParent.GatewayRef.Name,
			Namespace: existingParent.GatewayRef.Namespace,
		}

		isCurrentlyOurs := gatewayParents.Contains(parentKey)

		// Keep parents that are not ours and not default status parents
		if !isCurrentlyOurs &&
			!isOurManagedGateway(gateways, existingParent.GatewayRef.Namespace, existingParent.GatewayRef.Name) &&
			!isDefaultStatusParent(existingParent) {
			finalParents = append(finalParents, existingParent)
		}
	}

	// Calculate status for each of our gateway parents
	for gatewayParent := range gatewayParents {
		parentStatus := calculateSingleParentStatus(pool, gatewayParent, services, existingParents, routeList)
		finalParents = append(finalParents, parentStatus)
	}

	return &inferencev1alpha2.InferencePoolStatus{
		Parents: finalParents,
	}
}

// findGatewayParents finds all Gateway parents that reference this InferencePool through HTTPRoutes
func findGatewayParents(
	pool *inferencev1alpha2.InferencePool,
	routeList []*gateway.HTTPRoute,
) sets.Set[types.NamespacedName] {
	gatewayParents := sets.New[types.NamespacedName]()

	for _, route := range routeList {
		// Only process routes that reference our InferencePool
		if !routeReferencesInferencePool(route, pool) {
			continue
		}

		// Check the route's parent status to find accepted gateways
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
func routeReferencesInferencePool(route *gateway.HTTPRoute, pool *inferencev1alpha2.InferencePool) bool {
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

// calculateSingleParentStatus calculates the status for a single gateway parent
func calculateSingleParentStatus(
	pool *inferencev1alpha2.InferencePool,
	gatewayParent types.NamespacedName,
	services krt.Collection[*corev1.Service],
	existingParents []inferencev1alpha2.PoolStatus,
	routeList []*gateway.HTTPRoute,
) inferencev1alpha2.PoolStatus {
	// Find existing status for this parent to preserve some conditions
	var existingConditions []metav1.Condition
	for _, existingParent := range existingParents {
		if existingParent.GatewayRef.Name == gatewayParent.Name &&
			existingParent.GatewayRef.Namespace == gatewayParent.Namespace {
			existingConditions = existingParent.Conditions
			break
		}
	}

	// Filter to only keep conditions we manage
	filteredConditions := filterUsedConditions(existingConditions,
		inferencev1alpha2.InferencePoolConditionAccepted,
		inferencev1alpha2.InferencePoolConditionResolvedRefs)

	// Calculate Accepted status by checking HTTPRoute parent status
	acceptedStatus := calculateAcceptedStatus(pool, gatewayParent, routeList)

	// Calculate ResolvedRefs status
	resolvedRefsStatus := calculateResolvedRefsStatus(pool, services)

	// Build the final status
	return inferencev1alpha2.PoolStatus{
		GatewayRef: corev1.ObjectReference{
			APIVersion: gatewayv1.GroupVersion.String(),
			Kind:       gvk.Gateway.Kind,
			Namespace:  gatewayParent.Namespace,
			Name:       gatewayParent.Name,
		},
		Conditions: setConditions(pool.Generation, filteredConditions, map[string]*condition{
			string(inferencev1alpha2.InferencePoolConditionAccepted):     acceptedStatus,
			string(inferencev1alpha2.InferencePoolConditionResolvedRefs): resolvedRefsStatus,
		}),
	}
}

// calculateAcceptedStatus determines if the InferencePool is accepted by checking HTTPRoute parent status
func calculateAcceptedStatus(
	pool *inferencev1alpha2.InferencePool,
	gatewayParent types.NamespacedName,
	routeList []*gateway.HTTPRoute,
) *condition {
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
							return &condition{
								reason:  string(inferencev1alpha2.InferencePoolReasonAccepted),
								status:  metav1.ConditionTrue,
								message: "Referenced by an HTTPRoute accepted by the parentRef Gateway",
							}
						}
						return &condition{
							reason: string(inferencev1alpha2.InferencePoolReasonHTTPRouteNotAccepted),
							status: metav1.ConditionFalse,
							message: fmt.Sprintf("Referenced HTTPRoute %s/%s not accepted by Gateway %s/%s: %s",
								route.Namespace, route.Name, gatewayParent.Namespace, gatewayParent.Name, parentCondition.Message),
						}
					}
				}

				// If no Accepted condition found, treat as unknown (parent is listed in status)
				return &condition{
					reason:  string(inferencev1alpha2.InferencePoolReasonAccepted),
					status:  metav1.ConditionUnknown,
					message: "Referenced by an HTTPRoute unknown parentRef Gateway status",
				}
			}
		}
	}

	// If we get here, no HTTPRoute was found that references this InferencePool with this gateway as parent
	// This shouldn't happen in normal operation since we only call this for known gateway parents
	return &condition{
		reason: string(inferencev1alpha2.InferencePoolReasonHTTPRouteNotAccepted),
		status: metav1.ConditionFalse,
		message: fmt.Sprintf("No HTTPRoute found referencing this InferencePool with Gateway %s/%s as parent",
			gatewayParent.Namespace, gatewayParent.Name),
	}
}

// calculateResolvedRefsStatus determines the states of the ExtensionRef
// * if the kind is supported
// * if the extensionRef is defined
// * if the service exists in the same namespace as the InferencePool
func calculateResolvedRefsStatus(
	pool *inferencev1alpha2.InferencePool,
	services krt.Collection[*corev1.Service],
) *condition {
	// defaults to service
	if pool.Spec.ExtensionRef.Kind != nil && string(*pool.Spec.ExtensionRef.Kind) != gvk.Service.Kind {
		return &condition{
			reason:  string(inferencev1alpha2.InferencePoolReasonInvalidExtensionRef),
			status:  metav1.ConditionFalse,
			message: "Unsupported ExtensionRef kind " + string(*pool.Spec.ExtensionRef.Kind),
		}
	}
	if string(pool.Spec.ExtensionRef.Name) == "" {
		return &condition{
			reason:  string(inferencev1alpha2.InferencePoolReasonInvalidExtensionRef),
			status:  metav1.ConditionFalse,
			message: "ExtensionRef not defined",
		}
	}
	svc := ptr.Flatten(services.GetKey(fmt.Sprintf("%s/%s", pool.Namespace, pool.Spec.ExtensionRef.Name)))
	if svc == nil {
		return &condition{
			reason:  string(inferencev1alpha2.InferencePoolReasonInvalidExtensionRef),
			status:  metav1.ConditionFalse,
			message: "Referenced ExtensionRef not found " + string(pool.Spec.ExtensionRef.Name),
		}
	}
	return &condition{
		reason:  string(inferencev1alpha2.InferencePoolConditionResolvedRefs),
		status:  metav1.ConditionTrue,
		message: "Referenced ExtensionRef resolved successfully",
	}
}

// isDefaultStatusParent checks if this is a default status parent entry
func isDefaultStatusParent(parent inferencev1alpha2.PoolStatus) bool {
	return parent.GatewayRef.Kind == "Status" && parent.GatewayRef.Name == "default"
}

// isOurManagedGateway checks if a Gateway is managed by one of our supported controllers
// This is used to identify stale parent entries that we previously added but are no longer referenced by HTTPRoutes
func isOurManagedGateway(gateways krt.Collection[*gateway.Gateway], namespace, name string) bool {
	gtw := ptr.Flatten(gateways.GetKey(fmt.Sprintf("%s/%s", namespace, name)))
	if gtw == nil {
		return false
	}
	_, ok := builtinClasses[gtw.Spec.GatewayClassName]
	return ok
}

func filterUsedConditions(conditions []metav1.Condition, usedConditions ...inferencev1alpha2.InferencePoolConditionType) []metav1.Condition {
	var result []metav1.Condition
	for _, condition := range conditions {
		if slices.Contains(usedConditions, inferencev1alpha2.InferencePoolConditionType(condition.Type)) {
			result = append(result, condition)
		}
	}
	return result
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

func translateShadowServiceToService(existingLabels map[string]string, shadow shadowServiceInfo, extRef extRefInfo) *corev1.Service {
	// Create a new service object based on the shadow service info
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      shadow.key.Name,
			Namespace: shadow.key.Namespace,
			Labels: maps.MergeCopy(map[string]string{
				InferencePoolRefLabel:                shadow.poolName,
				InferencePoolExtensionRefSvc:         extRef.name,
				InferencePoolExtensionRefPort:        strconv.Itoa(int(extRef.port)),
				InferencePoolExtensionRefFailureMode: extRef.failureMode,
				constants.InternalServiceSemantics:   constants.ServiceSemanticsInferencePool,
			}, existingLabels),
		},
		Spec: corev1.ServiceSpec{
			Selector:  shadow.selector,
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: corev1.ClusterIPNone, // Headless service
			Ports: []corev1.ServicePort{ // adding dummy port, not used for anything
				{
					Protocol:   "TCP",
					Port:       int32(54321),
					TargetPort: intstr.FromInt(int(shadow.targetPort)),
				},
			},
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

func (c *Controller) reconcileShadowService(
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
func (c *Controller) canManageShadowServiceForInference(obj *corev1.Service) (bool, string) {
	if obj == nil {
		// No object exists, we can manage it
		return true, ""
	}

	_, inferencePoolManaged := obj.GetLabels()[InferencePoolRefLabel]
	// We can manage if it has no manager or if we are the manager
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
