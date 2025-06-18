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
	"time"

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
	"istio.io/istio/pkg/util/sets"
)

const (
	maxServiceNameLength          = 63
	hashSize                      = 8
	InferencePoolRefLabel         = "istio.io/inferencepool-name"
	InferencePoolExtensionRefSvc  = "istio.io/inferencepool-extension-service"
	InferencePoolExtensionRefPort = "istio.io/inferencepool-extension-port"
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
	name string
	port int32
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
			// First, let's build the shadow service
			extRef := extRefInfo{
				name: string(pool.Spec.ExtensionRef.Name),
			}
			if pool.Spec.ExtensionRef.PortNumber != nil {
				extRef.port = int32(*pool.Spec.ExtensionRef.PortNumber)
			} else {
				extRef.port = 9002 // Default port for the inference extension
			}

			svcName, err := InferencePoolServiceName(pool.Name)
			if err != nil {
				log.Errorf("failed to generate service name for InferencePool %s: %v", pool.Name, err)
				return nil, nil
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

			gatewayParentsToEnsure := sets.New[types.NamespacedName]()
			routeList := krt.Fetch(ctx, httpRoutes, krt.FilterIndex(routesByInferencePool, pool.Namespace+"/"+pool.Name))
			for _, r := range routeList {
				for _, rule := range r.Spec.Rules {
					for _, httpBackendRef := range rule.BackendRefs {
						if httpBackendRef.BackendRef.Group == nil || httpBackendRef.BackendRef.Kind == nil {
							continue
						}

						if string(*httpBackendRef.BackendRef.Group) == gvk.InferencePool.Group &&
							string(*httpBackendRef.BackendRef.Kind) == gvk.InferencePool.Kind &&
							string(httpBackendRef.BackendRef.Name) == pool.ObjectMeta.Name {

							// Check if the backendRef namespace matches the InferencePool namespace.
							// If BackendRef.Namespace is not specified, the backend is in the same namespace as the HTTPRoute's.
							backendRefNamespace := r.Namespace
							if httpBackendRef.BackendRef.Namespace != nil && *httpBackendRef.BackendRef.Namespace != "" {
								backendRefNamespace = string(*httpBackendRef.BackendRef.Namespace)
							}

							if backendRefNamespace == pool.Namespace {
								// If the backendRef points to the InferencePool in the correct namespace,
								// check the HTTPRoute's parent status.
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
			}

			extensionReferenceResolvedStatus := resolveExtensionRef(services, *pool)

			// Upate the parents status list;
			//   - remove ours that are no longer used
			//   - keep parents from other controllers as is
			//   - update our current parents
			existingParents := pool.Status.DeepCopy().Parents

			// All ours from this reconciliation in a default unknown state
			ourParents := []inferencev1alpha2.PoolStatus{}
			finalParents := []inferencev1alpha2.PoolStatus{}
			// Add all existing parents from other controllers
			for _, existingParent := range existingParents {
				if !isManagedGateway(gateways, existingParent) {
					finalParents = append(finalParents, existingParent)
				} else if gatewayParentsToEnsure.Contains(types.NamespacedName{Name: string(existingParent.GatewayRef.Name), Namespace: string(existingParent.GatewayRef.Namespace)}) {
					// only add our parents that are still referenced by an HTTPRoute
					ourParents = append(ourParents, existingParent)
				}
			}

			// Create new default parents if this is a new parent
			for gtw := range gatewayParentsToEnsure {
				found := false
				for _, ourExistingParent := range ourParents {
					if ourExistingParent.GatewayRef.Name == gtw.Name && ourExistingParent.GatewayRef.Namespace == gtw.Namespace {
						found = true
						break
					}
				}
				if !found {
					ourParents = append(ourParents, *defaultUnknownStatus(gtw.Name, gtw.Namespace, pool.Generation))
				}
			}

			// Add all our parents and update the conditions from previous default unknown state
			for _, ourParent := range ourParents {
				// TODO: Update this is there are ever more conditions to consider
				finalParents = append(finalParents, inferencev1alpha2.PoolStatus{
					GatewayRef: ourParent.GatewayRef,
					Conditions: setConditions(pool.Generation, ourParent.Conditions, map[string]*condition{
						string(inferencev1alpha2.InferencePoolConditionAccepted): {
							reason:  string(inferencev1alpha2.InferencePoolReasonAccepted),
							status:  metav1.ConditionTrue,
							message: "Referenced by an HTTPRoute accepted by the parentRef Gateway",
						},
						string(inferencev1alpha2.ModelConditionResolvedRefs): extensionReferenceResolvedStatus,
					}),
				})
			}

			ipoolStatus := inferencev1alpha2.InferencePoolStatus{
				Parents: finalParents,
			}

			return &ipoolStatus, &InferencePool{
				shadowService:  shadowSvcInfo,
				extRef:         extRef,
				gatewayParents: gatewayParentsToEnsure,
			}
		}, opts.WithName("InferenceExtension")...)
}

// resolveExtensionRef checks if the extension ref is valid and returns a condition
// checks if the kind is supported and if the service exists in the same namespace as the InferencePool
func resolveExtensionRef(services krt.Collection[*corev1.Service], pool inferencev1alpha2.InferencePool) *condition {
	// defaults to service
	if pool.Spec.ExtensionRef.Kind != nil && string(*pool.Spec.ExtensionRef.Kind) != gvk.Service.Kind {
		return &condition{
			reason:  string(inferencev1alpha2.ModelReasonInvalidExtensionRef),
			status:  metav1.ConditionFalse,
			message: "Unsupported ExtensionRef kind " + string(*pool.Spec.ExtensionRef.Kind),
		}
	}
	if string(pool.Spec.ExtensionRef.Name) == "" {
		return &condition{
			reason:  string(inferencev1alpha2.ModelReasonInvalidExtensionRef),
			status:  metav1.ConditionFalse,
			message: "ExtensionRef not defined",
		}
	}
	svc := ptr.Flatten(services.GetKey(fmt.Sprintf("%s/%s", pool.Namespace, pool.Spec.ExtensionRef.Name)))
	if svc == nil {
		return &condition{
			reason:  string(inferencev1alpha2.ModelReasonInvalidExtensionRef),
			status:  metav1.ConditionFalse,
			message: "Referenced ExtensionRef not found " + string(pool.Spec.ExtensionRef.Name),
		}
	}
	return &condition{
		reason:  string(inferencev1alpha2.ModelConditionResolvedRefs),
		status:  metav1.ConditionTrue,
		message: "Referenced ExtensionRef resolved successfully ",
	}
}

// isManagedGateway checks if the Gateway is controlled by this controller
func isManagedGateway(gateways krt.Collection[*gateway.Gateway], parent inferencev1alpha2.PoolStatus) bool {
	gtw := ptr.Flatten(gateways.GetKey(fmt.Sprintf("%s/%s", parent.GatewayRef.Namespace, parent.GatewayRef.Name)))
	if gtw == nil {
		return false
	}
	_, ok := builtinClasses[gtw.Spec.GatewayClassName]
	return ok
}

func defaultUnknownStatus(gwName, ns string, generation int64) *inferencev1alpha2.PoolStatus {
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
				Status:             metav1.ConditionUnknown,
				Reason:             string(inferencev1alpha2.InferencePoolReasonAccepted),
				Message:            "Unknown acceptance status",
				ObservedGeneration: generation,
				LastTransitionTime: metav1.NewTime(time.Now()),
			},
			{
				Type:               string(inferencev1alpha2.ModelConditionResolvedRefs),
				Status:             metav1.ConditionUnknown,
				Reason:             string(inferencev1alpha2.ModelReasonResolvedRefs),
				Message:            "Unknown resolved refs status",
				ObservedGeneration: generation,
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

func translateShadowServiceToService(existingLabels map[string]string, shadow shadowServiceInfo, extRef extRefInfo) *corev1.Service {
	// Create a new service object based on the shadow service info
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      shadow.key.Name,
			Namespace: shadow.key.Namespace,
			Labels: maps.MergeCopy(map[string]string{
				InferencePoolRefLabel:              shadow.poolName,
				InferencePoolExtensionRefSvc:       extRef.name,
				InferencePoolExtensionRefPort:      strconv.Itoa(int(extRef.port)),
				constants.InternalServiceSemantics: constants.ServiceSemanticsInferencePool,
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
	inferencePools krt.Collection[InferencePool],
	servicesCollection krt.Collection[*corev1.Service],
) func(key types.NamespacedName) error {
	services := kclient.NewFiltered[*corev1.Service](c.client, kclient.Filter{
		ObjectFilter: c.client.ObjectFilter(),
	})
	return func(key types.NamespacedName) error {
		// Find the InferencePool that matches the key
		pool := inferencePools.GetKey(key.String())
		if pool == nil {
			log.Debugf("skipping reconciliation for key %s, no InferencePool found", key.String())
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
			_, err = services.Create(service)
		} else {
			// TODO: Don't overwrite resources: https://github.com/istio/istio/issues/56667
			service.ResourceVersion = existingService.ResourceVersion
			_, err = services.Update(service)
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
			if string(*backendRef.BackendRef.Group) == gvk.InferencePool.Group &&
				string(*backendRef.BackendRef.Kind) == gvk.InferencePool.Kind {
				// If BackendRef.Namespace is not specified, the backend is in the same namespace as the HTTPRoute's
				backendRefNamespace := o.Namespace
				if backendRef.BackendRef.Namespace != nil && *backendRef.BackendRef.Namespace != "" {
					backendRefNamespace = string(*backendRef.BackendRef.Namespace)
				}
				key := backendRefNamespace + "/" + string(backendRef.Name)
				keys = append(keys, key)
			}
		}
	}
	return keys
}
