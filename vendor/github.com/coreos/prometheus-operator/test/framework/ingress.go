// Copyright 2016 The prometheus-operator Authors
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

package framework

import (
	"fmt"
	"os"
	"time"

	"github.com/pkg/errors"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
)

func MakeBasicIngress(serviceName string, servicePort int) *v1beta1.Ingress {
	return &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "monitoring",
		},
		Spec: v1beta1.IngressSpec{
			Rules: []v1beta1.IngressRule{
				{
					IngressRuleValue: v1beta1.IngressRuleValue{
						HTTP: &v1beta1.HTTPIngressRuleValue{
							Paths: []v1beta1.HTTPIngressPath{
								{
									Backend: v1beta1.IngressBackend{
										ServiceName: serviceName,
										ServicePort: intstr.FromInt(servicePort),
									},
									Path: "/metrics",
								},
							},
						},
					},
				},
			},
		},
	}
}

func CreateIngress(kubeClient kubernetes.Interface, namespace string, i *v1beta1.Ingress) error {
	_, err := kubeClient.Extensions().Ingresses(namespace).Create(i)
	return errors.Wrap(err, fmt.Sprintf("creating ingress %v failed", i.Name))
}

func SetupNginxIngressControllerIncDefaultBackend(kubeClient kubernetes.Interface, namespace string) error {
	// Create Nginx Ingress Replication Controller
	if err := createReplicationControllerViaYml(kubeClient, namespace, "./framework/ressources/nxginx-ingress-controller.yml"); err != nil {
		return errors.Wrap(err, "creating nginx ingress replication controller failed")
	}

	// Create Default HTTP Backend Replication Controller
	if err := createReplicationControllerViaYml(kubeClient, namespace, "./framework/ressources/default-http-backend.yml"); err != nil {
		return errors.Wrap(err, "creating default http backend replication controller failed")
	}

	// Create Default HTTP Backend Service
	manifest, err := os.Open("./framework/ressources/default-http-backend-service.yml")
	if err != nil {
		return errors.Wrap(err, "reading default http backend service yaml failed")
	}

	service := v1.Service{}
	err = yaml.NewYAMLOrJSONDecoder(manifest, 100).Decode(&service)
	if err != nil {
		return errors.Wrap(err, "decoding http backend service yaml failed")
	}

	_, err = kubeClient.CoreV1().Services(namespace).Create(&service)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("creating http backend service %v failed", service.Name))
	}
	if err := WaitForServiceReady(kubeClient, namespace, service.Name); err != nil {
		return errors.Wrap(err, fmt.Sprintf("waiting for http backend service %v timed out", service.Name))
	}

	return nil
}

func DeleteNginxIngressControllerIncDefaultBackend(kubeClient kubernetes.Interface, namespace string) error {
	// Delete Nginx Ingress Replication Controller
	if err := deleteReplicationControllerViaYml(kubeClient, namespace, "./framework/ressources/nxginx-ingress-controller.yml"); err != nil {
		return errors.Wrap(err, "deleting nginx ingress replication controller failed")
	}

	// Delete Default HTTP Backend Replication Controller
	if err := deleteReplicationControllerViaYml(kubeClient, namespace, "./framework/ressources/default-http-backend.yml"); err != nil {
		return errors.Wrap(err, "deleting default http backend replication controller failed")
	}

	// Delete Default HTTP Backend Service
	manifest, err := os.Open("./framework/ressources/default-http-backend-service.yml")
	if err != nil {
		return errors.Wrap(err, "reading default http backend service yaml failed")
	}

	service := v1.Service{}
	err = yaml.NewYAMLOrJSONDecoder(manifest, 100).Decode(&service)
	if err != nil {
		return errors.Wrap(err, "decoding http backend service yaml failed")
	}

	if err := kubeClient.CoreV1().Services(namespace).Delete(service.Name, nil); err != nil {
		return errors.Wrap(err, fmt.Sprintf("deleting http backend service %v failed", service.Name))
	}

	return nil
}

func GetIngressIP(kubeClient kubernetes.Interface, namespace string, ingressName string) (*string, error) {
	var ingress *v1beta1.Ingress
	err := wait.Poll(time.Millisecond*500, time.Minute*5, func() (bool, error) {
		var err error
		ingress, err = kubeClient.Extensions().Ingresses(namespace).Get(ingressName, metav1.GetOptions{})
		if err != nil {
			return false, errors.Wrap(err, fmt.Sprintf("requesting the ingress %v failed", ingressName))
		}
		ingresses := ingress.Status.LoadBalancer.Ingress
		if len(ingresses) != 0 {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return &ingress.Status.LoadBalancer.Ingress[0].IP, nil
}
