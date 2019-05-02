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
	"os"
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
)

func createReplicationControllerViaYml(kubeClient kubernetes.Interface, namespace string, filepath string) error {
	manifest, err := os.Open(filepath)
	if err != nil {
		return err
	}

	var rC v1.ReplicationController
	err = yaml.NewYAMLOrJSONDecoder(manifest, 100).Decode(&rC)
	if err != nil {
		return err
	}

	_, err = kubeClient.CoreV1().ReplicationControllers(namespace).Create(&rC)
	if err != nil {
		return err
	}

	return nil
}

func deleteReplicationControllerViaYml(kubeClient kubernetes.Interface, namespace string, filepath string) error {
	manifest, err := os.Open(filepath)
	if err != nil {
		return err
	}

	var rC v1.ReplicationController
	err = yaml.NewYAMLOrJSONDecoder(manifest, 100).Decode(&rC)
	if err != nil {
		return err
	}

	if err := scaleDownReplicationController(kubeClient, namespace, rC); err != nil {
		return err
	}

	if err := kubeClient.CoreV1().ReplicationControllers(namespace).Delete(rC.Name, nil); err != nil {
		return err
	}

	return nil
}

func scaleDownReplicationController(kubeClient kubernetes.Interface, namespace string, rC v1.ReplicationController) error {
	*rC.Spec.Replicas = 0
	rCAPI := kubeClient.CoreV1().ReplicationControllers(namespace)

	_, err := kubeClient.CoreV1().ReplicationControllers(namespace).Update(&rC)
	if err != nil {
		return err
	}

	return wait.Poll(time.Second, time.Minute*5, func() (bool, error) {
		currentRC, err := rCAPI.Get(rC.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		if currentRC.Status.Replicas == 0 {
			return true, nil
		}
		return false, nil
	})
}
