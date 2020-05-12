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
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
func InsertDataToConfigMap(client corev1.ConfigMapsGetter, meta metav1.ObjectMeta, data map[string]string) error {
	configmap, err := client.ConfigMaps(meta.Namespace).Get(context.TODO(), meta.Name, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("error when getting configmap %v: %v", meta.Name, err)
	}
	if errors.IsNotFound(err) {
		// Create a new ConfigMap.
		configmap = &v1.ConfigMap{
			ObjectMeta: meta,
			Data:       data,
		}
		if _, err = client.ConfigMaps(meta.Namespace).Create(context.TODO(), configmap, metav1.CreateOptions{}); err != nil {
			if errors.IsNotFound(err) {
				// Namespace not found, ignore
				return nil
			}
			if errors.IsForbidden(err) {
				// This may happen if the namespace is deleting, or if we do not have RBAC permissions
				// In both cases, this is not retryable.
				// Warn here for visibility; if they get RBAC permissions later it will be handled by the resync
				log.Warnf("failed to create namespace in %v: %v", meta.Namespace, err)
				return nil
			}
			if errors.IsAlreadyExists(err) {
				// Another instance may have created this
				return nil
			}
			return fmt.Errorf("error when creating configmap %v: %+v", meta.Name, err)
		}
	} else {
		// Otherwise, update the config map if changes are required
		err := UpdateDataInConfigMap(client, configmap, data)
		if err != nil {
			return err
		}
	}
	return nil
}

// insertData merges a configmap with a map, and returns true if any changes were made
func insertData(cm *v1.ConfigMap, data map[string]string) bool {
	if cm.Data == nil {
		cm.Data = data
		return true
	}
	needsUpdate := false
	for k, v := range data {
		if cm.Data[k] != v {
			needsUpdate = true
		}
		cm.Data[k] = v
	}
	return needsUpdate
}

func UpdateDataInConfigMap(client corev1.ConfigMapsGetter, cm *v1.ConfigMap, data map[string]string) error {
	if cm == nil {
		return fmt.Errorf("cannot update nil configmap")
	}
	if needsUpdate := insertData(cm, data); !needsUpdate {
		return nil
	}
	if _, err := client.ConfigMaps(cm.Namespace).Update(context.TODO(), cm, metav1.UpdateOptions{}); err != nil {
		if errors.IsConflict(err) {
			log.Warnf("conflict updating configmap for %v", cm.Namespace)
			return nil
		}
		return fmt.Errorf("error when updating configmap %v: %v", cm.Name, err)
	}
	return nil
}
