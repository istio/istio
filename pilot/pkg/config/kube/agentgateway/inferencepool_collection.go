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
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/util/sets"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
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

func (c *Controller) canManageShadowServiceForInference(obj *corev1.Service) (bool, string) {
	if obj == nil {
		// No object exists, we can manage it
		return true, ""
	}

	_, inferencePoolManaged := obj.GetLabels()[InferencePoolRefLabel]
	// We can manage if it has no manager or if we are the manager
	return inferencePoolManaged, obj.GetResourceVersion()
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
			logger.Debugf("inferencepool no longer exists", key.String())
			return nil
		}

		// We found the InferencePool, now we need to translate it to a shadow Service
		// and check if it exists already
		existingService := ptr.Flatten(servicesCollection.GetKey(pool.shadowService.key.String()))

		// Check if we can manage this service
		if existingService != nil {
			canManage, reason := c.canManageShadowServiceForInference(existingService)
			if !canManage {
				logger.Debugf("skipping service %s/%s, already managed by another controller: %s", key.Namespace, key.Name, reason)
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
