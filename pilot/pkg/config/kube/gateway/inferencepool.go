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

	istiolog "istio.io/istio/pkg/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	inferencev1alpha2 "sigs.k8s.io/gateway-api-inference-extension/api/v1alpha2"

	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
)

const maxServiceNameLength = 63
const hashSize = 8
const InferencePoolRefLabel = "inference.x-k8s.io/inference-pool-name"
const InferencePoolExtensionRefSvc = "inference.x-k8s.io/extension-service"
const InferencePoolExtensionRefPort = "inference.x-k8s.io/extension-port"

// // ManagedLabel is the label used to identify resources managed by this controller
// const ManagedLabel = "inference.x-k8s.io/managed-by"

// ControllerName is the name of this controller for labeling resources it manages
const ControllerName = "inference-controller"

// InferencePoolController implements a controller that materializes an InferencePool
// into a Kubernetes Service to allow traffic to the inference pool.
type InferencePoolController struct {
	client                         kube.Client
	queue                          controllers.Queue
	patcher                        patcher
	pools                          kclient.Client[*inferencev1alpha2.InferencePool]
	services                       kclient.Client[*corev1.Service]
	ServiceToEndpointPickerService map[types.NamespacedName]*corev1.Service
	clients                        map[schema.GroupVersionResource]getter
}

// NewInferencePoolController constructs a new InferencePoolController and registers required informers.
func NewInferencePoolController(client kube.Client) *InferencePoolController {
	filter := kclient.Filter{ObjectFilter: client.ObjectFilter()}
	pools := kclient.NewFiltered[*inferencev1alpha2.InferencePool](client, filter)

	ic := &InferencePoolController{
		client:  client,
		clients: map[schema.GroupVersionResource]getter{},
		patcher: func(gvr schema.GroupVersionResource, name string, namespace string, data []byte, subresources ...string) error {
			c := client.Dynamic().Resource(gvr).Namespace(namespace)
			t := true
			_, err := c.Patch(context.Background(), name, types.ApplyPatchType, data, metav1.PatchOptions{
				Force:        &t,
				FieldManager: ControllerName,
			}, subresources...)
			return err
		},
		pools: pools,
	}

	ic.queue = controllers.NewQueue("inference pool",
		controllers.WithReconciler(ic.Reconcile),
		controllers.WithMaxAttempts(5))

	// Set up a handler that will add the parent InferencePool object onto the queue
	parentHandler := controllers.ObjectHandler(controllers.EnqueueForParentHandler(ic.queue, gvk.InferencePool))

	ic.services = kclient.NewFiltered[*corev1.Service](client, filter)
	ic.services.AddEventHandler(parentHandler)
	ic.clients[gvr.Service] = NewUntypedWrapper(ic.services)

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
	)
	ic.queue.Run(stop)
	controllers.ShutdownAll(ic.pools, ic.services)
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

	// if !isManaged(pool) {
	// 	log.Debugf("inference pool is not managed by this controller")
	// 	return nil
	// }

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

	err = ic.apply(service)
	if err != nil {
		return fmt.Errorf("failed to apply Service: %v", err)
	}

	log.Info("inference pool service updated")
	return nil
}

// generateHash generates an 8-character SHA256 hash of the input string.
func generateHash(input string, length int) string {
	hashBytes := sha256.Sum256([]byte(input))
	hashString := fmt.Sprintf("%x", hashBytes) // Convert to hexadecimal string
	return hashString[:length]                 // Truncate to desired length
}

func InferencePoolServiceName(poolName string) string {
	hash := generateHash(poolName, hashSize)
	svcName := fmt.Sprintf("%s-ip-%s", poolName, hash)
	// Truncate if necessary to meet the Kubernetes naming constraints
	if len(svcName) > maxServiceNameLength {
		// Calculate the maximum allowed base name length
		maxBaseLength := maxServiceNameLength - len("-ip-") - hashSize
		// if maxBaseLength < 0 {
		// 	return nil, fmt.Errorf("inference pool name: %s is too long", pool.Name)
		// }

		// Truncate the base name and reconstruct the service name
		truncatedBase := poolName[:maxBaseLength]
		svcName = fmt.Sprintf("%s-ip-%s", truncatedBase, hash)
	}
	return svcName
}

// translateInferencePoolToService converts an InferencePool to a Service
func translateInferencePoolToService(pool *inferencev1alpha2.InferencePool) (*corev1.Service, error) {
	svcName := InferencePoolServiceName(pool.Name)

	// if err != nil {
	// 	retErr := fmt.Errorf("cannot generate service name for InferencePool %s/%s: %w", inferPool.GetNamespace(), inferPool.GetName(), err)
	// 	r.Logger.Errorf(retErr.Error())
	// 	r.Metrics.IncrementReconcilerErrors("Reconciler.processInferencePool", r.reconcilerType)
	// 	return retErr
	// }

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
	//TODO: for now, supporting only service
	extRef := pool.Spec.ExtensionRef
	shadowSvc.Labels[InferencePoolExtensionRefSvc] = string(extRef.Name)
	if extRef.PortNumber != nil {
		shadowSvc.Labels[InferencePoolExtensionRefPort] = strconv.Itoa(int(*extRef.PortNumber))
	} else {
		shadowSvc.Labels[InferencePoolExtensionRefPort] = "9002"
	}
	shadowSvc.Labels["internal.istio.io/service-semantics"] = "inferencepool"
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
	gvk := pool.GetObjectKind().GroupVersionKind()
	log.Infof("LIOR111: %v", gvk)
	shadowSvc.SetOwnerReferences([]metav1.OwnerReference{
		{
			// APIVersion: pool.APIVersion,
			APIVersion: "inference.networking.x-k8s.io/v1alpha2",
			Kind:       "InferencePool",
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

// apply server-side applies a resource to the cluster
func (ic *InferencePoolController) apply(obj interface{}) error {
	// Convert to unstructured
	log.Infof("LIOR111: \n%v", obj)
	gvr := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "services",
	}
	if service, ok := obj.(*corev1.Service); ok {
		name := service.Name
		namespace := service.Namespace
		// Get the existing service if it exists
		existingService := ic.services.Get(service.Name, service.Namespace)

		// Check if we can manage this service
		if existingService != nil {
			canManage, _ := ic.canManage(gvr, existingService.Name, existingService.Namespace)
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
	log.Info("NOT A SERVICE")
	return nil
}

// canManage checks if a resource should be managed by this controller
func (ic *InferencePoolController) canManage(gvr schema.GroupVersionResource, name, namespace string) (bool, string) {
	store, f := ic.clients[gvr]
	if !f {
		log.Warnf("unknown GVR %v", gvr)
		// Allow management even if we don't know the type
		return true, ""
	}

	obj := store.Get(name, namespace)
	if obj == nil {
		// No object exists, we can manage it
		return true, ""
	}

	_, inferencePoolManaged := obj.GetLabels()[InferencePoolRefLabel]
	// We can manage if it has no manager or if we are the manager
	return inferencePoolManaged, obj.GetResourceVersion()
}
