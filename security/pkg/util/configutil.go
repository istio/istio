// Copyright 2020 Istio Authors
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

package util

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/pkg/log"
)

// InsertDataToConfigMap inserts a data to a configmap in a namespace.
// client: the k8s client interface.
// namespace: the namespace of the configmap.
// value: the value of the data to insert.
// configName: the name of the configmap.
// dataName: the name of the data in the configmap.
func InsertDataToConfigMap(client corev1.ConfigMapsGetter, namespace, value, configName, dataName string) error {
	configmap, err := client.ConfigMaps(namespace).Get(configName, metav1.GetOptions{})
	exists := true
	if err != nil {
		if errors.IsNotFound(err) {
			// Create a new ConfigMap.
			configmap = &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configName,
					Namespace: namespace,
				},
				Data: map[string]string{},
			}
			exists = false
		} else {
			return fmt.Errorf("error when getting configmap %v: %v", configName, err)
		}
	}
	configmap.Data[dataName] = value
	if exists {
		if _, err = client.ConfigMaps(namespace).Update(configmap); err != nil {
			return fmt.Errorf("error when updating configmap %v: %v", configName, err)
		}
	} else {
		if _, err = client.ConfigMaps(namespace).Create(configmap); err != nil {
			return fmt.Errorf("error when creating configmap %v: %v", configName, err)
		}
	}
	return nil
}

// InsertDataToConfigMapWithRetry inserts a data to a configmap in a namespace with a
// retry mechanism.
// client: the k8s client interface.
// namespace: the namespace of the configmap.
// value: the value of the data to insert.
// configName: the name of the configmap.
// dataName: the name of the data in the configmap.
// retryInterval: the retry interval.
// timeout: the timeout for the retry.
func InsertDataToConfigMapWithRetry(client corev1.ConfigMapsGetter, namespace, value,
	configName, dataName string, retryInterval, timeout time.Duration) error {
	start := time.Now()
	for {
		err := InsertDataToConfigMap(client, namespace, value, configName, dataName)
		if err == nil {
			return nil
		}
		log.Errorf("error when inserting data to config map %v: %v", configName, err)

		if time.Since(start) > timeout {
			log.Errorf("timeout for inserting data to config map %v", configName)
			return err
		}
		time.Sleep(retryInterval)
	}
}
