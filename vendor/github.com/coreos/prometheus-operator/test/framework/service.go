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
	"time"

	"github.com/pkg/errors"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

func CreateServiceAndWaitUntilReady(kubeClient kubernetes.Interface, namespace string, service *v1.Service) (finalizerFn, error) {
	finalizerFn := func() error { return DeleteServiceAndWaitUntilGone(kubeClient, namespace, service.Name) }

	if _, err := kubeClient.CoreV1().Services(namespace).Create(service); err != nil {
		return finalizerFn, errors.Wrap(err, fmt.Sprintf("creating service %v failed", service.Name))
	}

	if err := WaitForServiceReady(kubeClient, namespace, service.Name); err != nil {
		return finalizerFn, errors.Wrap(err, fmt.Sprintf("waiting for service %v to become ready timed out", service.Name))
	}
	return finalizerFn, nil
}

func WaitForServiceReady(kubeClient kubernetes.Interface, namespace string, serviceName string) error {
	err := wait.Poll(time.Second, time.Minute*5, func() (bool, error) {
		endpoints, err := getEndpoints(kubeClient, namespace, serviceName)
		if err != nil {
			return false, err
		}
		if len(endpoints.Subsets) != 0 && len(endpoints.Subsets[0].Addresses) > 0 {
			return true, nil
		}
		return false, nil
	})
	return err
}

func DeleteServiceAndWaitUntilGone(kubeClient kubernetes.Interface, namespace string, serviceName string) error {
	if err := kubeClient.CoreV1().Services(namespace).Delete(serviceName, nil); err != nil {
		return errors.Wrap(err, fmt.Sprintf("deleting service %v failed", serviceName))
	}

	err := wait.Poll(5*time.Second, time.Minute, func() (bool, error) {
		_, err := getEndpoints(kubeClient, namespace, serviceName)
		if err != nil {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return errors.Wrap(err, "waiting for service to go away failed")
	}

	return nil
}

func getEndpoints(kubeClient kubernetes.Interface, namespace, serviceName string) (*v1.Endpoints, error) {
	endpoints, err := kubeClient.CoreV1().Endpoints(namespace).Get(serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("requesting endpoints for servce %v failed", serviceName))
	}
	return endpoints, nil
}
