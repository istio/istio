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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/json"
	inferencev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
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
	InferencePoolFieldManager            = "istio.io/inference-pool-controller"
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
	httpRoutes krt.Collection[*gatewayv1.HTTPRoute],
	gateways krt.Collection[*gatewayv1.Gateway],
	routesByInferencePool krt.Index[string, *gatewayv1.HTTPRoute],
	c *Controller,
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

			// Calculate status
			status := calculateInferencePoolStatus(pool, gatewayParents, services, gateways, routeList)

			return status, inferencePool
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

// calculateInferencePoolStatus calculates the complete status for an InferencePool
func calculateInferencePoolStatus(
	pool *inferencev1.InferencePool,
	gatewayParents sets.Set[types.NamespacedName],
	services krt.Collection[*corev1.Service],
	gateways krt.Collection[*gatewayv1.Gateway],
	routeList []*gatewayv1.HTTPRoute,
) *inferencev1.InferencePoolStatus {
	// Calculate status for each gateway parent
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

		// Keep parents that are not ours and not default status parents
		if !isCurrentlyOurs &&
			!isOurManagedGateway(gateways, gtwNamespace, gtwName) &&
			!isDefaultStatusParent(existingParent) {
			finalParents = append(finalParents, existingParent)
		}
	}

	// Calculate status for each of our gateway parents
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
	routeList []*gatewayv1.HTTPRoute,
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
func routeReferencesInferencePool(route *gatewayv1.HTTPRoute, pool *inferencev1.InferencePool) bool {
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
	pool *inferencev1.InferencePool,
	gatewayParent types.NamespacedName,
	services krt.Collection[*corev1.Service],
	existingParents []inferencev1.ParentStatus,
	routeList []*gatewayv1.HTTPRoute,
) inferencev1.ParentStatus {
	// Find existing status for this parent to preserve some conditions
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

	// Calculate Accepted status by checking HTTPRoute parent status
	acceptedStatus := calculateAcceptedStatus(pool, gatewayParent, routeList)

	// Calculate ResolvedRefs status
	resolvedRefsStatus := calculateResolvedRefsStatus(pool, services)

	// Build the final status
	return inferencev1.ParentStatus{
		ParentRef: inferencev1.ParentReference{
			Group:     (*inferencev1.Group)(&gvk.Gateway.Group),
			Kind:      inferencev1.Kind(gvk.Gateway.Kind),
			Namespace: inferencev1.Namespace(gatewayParent.Namespace),
			Name:      inferencev1.ObjectName(gatewayParent.Name),
		},
		Conditions: setConditions(pool.Generation, filteredConditions, map[string]*condition{
			string(inferencev1.InferencePoolConditionAccepted):     acceptedStatus,
			string(inferencev1.InferencePoolConditionResolvedRefs): resolvedRefsStatus,
		}),
	}
}

// calculateAcceptedStatus determines if the InferencePool is accepted by checking HTTPRoute parent status
func calculateAcceptedStatus(
	pool *inferencev1.InferencePool,
	gatewayParent types.NamespacedName,
	routeList []*gatewayv1.HTTPRoute,
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
								reason:  string(inferencev1.InferencePoolReasonAccepted),
								status:  metav1.ConditionTrue,
								message: "Referenced by an HTTPRoute accepted by the parentRef Gateway",
							}
						}
						return &condition{
							reason: string(inferencev1.InferencePoolReasonHTTPRouteNotAccepted),
							status: metav1.ConditionFalse,
							message: fmt.Sprintf("Referenced HTTPRoute %s/%s not accepted by Gateway %s/%s: %s",
								route.Namespace, route.Name, gatewayParent.Namespace, gatewayParent.Name, parentCondition.Message),
						}
					}
				}

				// If no Accepted condition found, treat as unknown (parent is listed in status)
				return &condition{
					reason:  string(inferencev1.InferencePoolReasonAccepted),
					status:  metav1.ConditionUnknown,
					message: "Referenced by an HTTPRoute unknown parentRef Gateway status",
				}
			}
		}
	}

	// If we get here, no HTTPRoute was found that references this InferencePool with this gateway as parent
	// This shouldn't happen in normal operation since we only call this for known gateway parents
	return &condition{
		reason: string(inferencev1.InferencePoolReasonHTTPRouteNotAccepted),
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
	pool *inferencev1.InferencePool,
	services krt.Collection[*corev1.Service],
) *condition {
	// Default Kind to Service if unset
	kind := string(pool.Spec.EndpointPickerRef.Kind)
	if kind == "" {
		kind = gvk.Service.Kind
	}

	if kind != gvk.Service.Kind {
		return &condition{
			reason:  string(inferencev1.InferencePoolReasonInvalidExtensionRef),
			status:  metav1.ConditionFalse,
			message: "Unsupported ExtensionRef kind " + kind,
		}
	}

	name := string(pool.Spec.EndpointPickerRef.Name)
	if name == "" {
		return &condition{
			reason:  string(inferencev1.InferencePoolReasonInvalidExtensionRef),
			status:  metav1.ConditionFalse,
			message: "ExtensionRef not defined",
		}
	}

	svc := ptr.Flatten(services.GetKey(fmt.Sprintf("%s/%s", pool.Namespace, name)))
	if svc == nil {
		return &condition{
			reason:  string(inferencev1.InferencePoolReasonInvalidExtensionRef),
			status:  metav1.ConditionFalse,
			message: "Referenced ExtensionRef not found " + name,
		}
	}

	return &condition{
		reason:  string(inferencev1.InferencePoolReasonResolvedRefs),
		status:  metav1.ConditionTrue,
		message: "Referenced ExtensionRef resolved successfully",
	}
}

// isDefaultStatusParent checks if this is a default status parent entry
func isDefaultStatusParent(parent inferencev1.ParentStatus) bool {
	return string(parent.ParentRef.Kind) == "Status" && parent.ParentRef.Name == "default"
}

// isOurManagedGateway checks if a Gateway is managed by one of our supported controllers
// This is used to identify stale parent entries that we previously added but are no longer referenced by HTTPRoutes
func isOurManagedGateway(gateways krt.Collection[*gatewayv1.Gateway], namespace, name string) bool {
	gtw := ptr.Flatten(gateways.GetKey(fmt.Sprintf("%s/%s", namespace, name)))
	if gtw == nil {
		return false
	}
	_, ok := builtinClasses[gtw.Spec.GatewayClassName]
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

func translateShadowServiceToService(shadow shadowServiceInfo, extRef extRefInfo) *corev1.Service {
	// Create multiple ports for the shadow service - one for each InferencePool targetPort.
	// This allows Istio to discover endpoints for all targetPorts.
	// We use dummy service ports (54321, 54322, etc.) that map to the actual targetPorts.
	baseDummyPort := int32(54321)
	ports := make([]corev1.ServicePort, 0, len(shadow.targetPorts))

	for i, tp := range shadow.targetPorts {
		portName := fmt.Sprintf("http-%d", i)
		ports = append(ports, corev1.ServicePort{
			Name:       portName,
			Protocol:   corev1.ProtocolTCP,
			Port:       baseDummyPort + int32(i),
			TargetPort: intstr.FromInt(int(tp.port)),
		})
	}

	// Create a new service object based on the shadow service info
	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      shadow.key.Name,
			Namespace: shadow.key.Namespace,
			Labels: map[string]string{
				InferencePoolRefLabel:                shadow.poolName,
				InferencePoolExtensionRefSvc:         extRef.name,
				InferencePoolExtensionRefPort:        strconv.Itoa(int(extRef.port)),
				InferencePoolExtensionRefFailureMode: extRef.failureMode,
				constants.InternalServiceSemantics:   constants.ServiceSemanticsInferencePool,
			},
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

func (c *Controller) reconcileShadowService(
	kubeClient kube.Client,
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
		if existingService != nil {
			canManage, reason := c.canManageShadowServiceForInference(existingService)
			if !canManage {
				log.Debugf("skipping service %s/%s, already managed by another controller: %s", key.Namespace, key.Name, reason)
				return nil
			}
		}

		service := translateShadowServiceToService(pool.shadowService, pool.extRef)
		return c.applyShadowService(kubeClient, service)
	}
}

// applyShadowService uses Server-Side Apply to create or update shadow services
func (c *Controller) applyShadowService(kubeClient kube.Client, service *corev1.Service) error {
	data, err := json.Marshal(service)
	if err != nil {
		return fmt.Errorf("failed to marshal service for SSA: %v", err)
	}

	ctx := context.Background()
	_, err = kubeClient.Kube().CoreV1().Services(service.Namespace).Patch(
		ctx, service.Name, types.ApplyPatchType, data, metav1.PatchOptions{
			FieldManager: InferencePoolFieldManager,
			Force:        ptr.Of(true),
		})
	return err
}

func (c *Controller) canManageShadowServiceForInference(obj *corev1.Service) (bool, string) {
	if obj == nil {
		// No object exists, we can manage it
		return true, ""
	}

	_, inferencePoolManaged := obj.GetLabels()[InferencePoolRefLabel]
	// We can manage if it has no manager or if we are the manager
	return inferencePoolManaged, obj.GetResourceVersion()
}

func indexHTTPRouteByInferencePool(o *gatewayv1.HTTPRoute) []string {
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
