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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	inferencev1alpha2 "sigs.k8s.io/gateway-api-inference-extension/api/v1alpha2"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
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

type shadowServiceInfo struct {
	key    types.NamespacedName
	labels map[string]string
}

type extRefInfo struct {
	name string
	port int32
}

type InferencePool struct {
	*inferencev1alpha2.InferencePool
	shadowService shadowServiceInfo
	extRef        extRefInfo
}

func InferencePoolCollection(
	pools krt.Collection[*inferencev1alpha2.InferencePool],
	services krt.Collection[*corev1.Service],
	httpRoutes krt.Collection[*gateway.HTTPRoute],
	gateways krt.Collection[*gateway.Gateway],
	routesByNamespace krt.Index[string, *gateway.HTTPRoute],
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
			shadowSvc, err := translateInferencePoolToService(pool, extRef)
			if err != nil {
				// N.B don't discard result here because an invalid name implies that there is no good state
				// to fall back to.
				log.Errorf("failed to generate shadow service for InferencePool %s: %v", pool.Name, err)
				return nil, nil
			}

			shadowSvcInfo := shadowServiceInfo{
				key: types.NamespacedName{
					Name:      shadowSvc.Name,
					Namespace: shadowSvc.Namespace,
				},
				labels: shadowSvc.GetLabels(),
			}

			// Attempt to create/update the shadow service
			err = c.reconcileShadowService(ctx, shadowSvc, services)
			if err != nil {
				// Fall back to last known good state
				ctx.DiscardResult()
				log.Errorf("failed to reconcile shadow service for InferencePool %s: %v", pool.Name, err)
				return nil, nil
			}

			gatewayParentsToEnsure := sets.New[types.NamespacedName]()
			routeList := krt.Fetch(ctx, httpRoutes, krt.FilterIndex(routesByNamespace, pool.Namespace))
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

			newParents := []inferencev1alpha2.PoolStatus{}
			for gtw := range gatewayParentsToEnsure {
				newParents = append(newParents, *poolStatusTmpl(gtw.Name, gtw.Namespace, pool.Generation))
			}

			ipoolStatus := inferencev1alpha2.InferencePoolStatus{
				Parents: newParents,
			}

			return &ipoolStatus, &InferencePool{
				InferencePool: pool,
				shadowService: shadowSvcInfo,
				extRef:        extRef,
			}
		}, opts.WithName("InferenceExtension")...)
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
func translateInferencePoolToService(pool *inferencev1alpha2.InferencePool, extRef extRefInfo) (*corev1.Service, error) {
	svcName, err := InferencePoolServiceName(pool.Name)
	if err != nil {
		return nil, err
	}

	shadowSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: pool.GetNamespace(),
			Labels: map[string]string{
				InferencePoolRefLabel:              pool.GetName(),
				InferencePoolExtensionRefSvc:       extRef.name,
				InferencePoolExtensionRefPort:      strconv.Itoa(int(extRef.port)),
				constants.InternalServiceSemantics: constants.ServiceSemanticsInferencePool,
			},
		},
		Spec: corev1.ServiceSpec{
			Selector:  make(map[string]string, len(pool.Spec.Selector)),
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: corev1.ClusterIPNone, // Headless service
			Ports: []corev1.ServicePort{ // adding dummy port, not used for anything
				{
					Protocol:   "TCP",
					Port:       int32(54321),
					TargetPort: intstr.FromInt(int(pool.Spec.TargetPortNumber)),
				},
			},
		},
	}

	for k, v := range pool.Spec.Selector {
		shadowSvc.Spec.Selector[string(k)] = string(v)
	}

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

func (c *Controller) reconcileShadowService(
	ctx krt.HandlerContext,
	service *corev1.Service,
	servicesCollection krt.Collection[*corev1.Service],
) error {
	// Get the existing service if it exists
	key := service.Namespace + "/" + service.Name
	// N.B we probably do want to subscribe to changes here e.g. so we reconcile if the
	// user changes the service manually.
	existingService := ptr.Flatten(krt.FetchOne(ctx, servicesCollection, krt.FilterKey(key)))

	// Check if we can manage this service
	if existingService != nil {
		canManage, _ := c.canManageShadowServiceForInference(existingService)
		if !canManage {
			log.Debugf("skipping service %s/%s, already managed by another controller", service.Namespace, service.Name)
			return nil
		}
	}

	var err error
	if existingService == nil {
		// Create the service if it doesn't exist
		_, err = c.client.Kube().CoreV1().Services(service.Namespace).Create(
			context.Background(), service, metav1.CreateOptions{})
	} else {
		// TODO: Don't overwrite resources: https://github.com/istio/istio/issues/56667
		service.ResourceVersion = existingService.ResourceVersion
		_, err = c.client.Kube().CoreV1().Services(service.Namespace).Update(
			context.Background(), service, metav1.UpdateOptions{})
	}

	if err != nil {
		return fmt.Errorf("failed to apply service %s/%s: %v", service.Namespace, service.Name, err)
	}
	return nil
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
