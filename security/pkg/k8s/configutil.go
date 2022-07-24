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

package k8s

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"

	"istio.io/istio/pkg/config/constants"
)

// InsertDataToConfigMap inserts a data to a configmap in a namespace.
// client: the k8s client interface.
// lister: the configmap lister.
// meta: the metadata of configmap.
// caBundle: ca cert data bytes.
func InsertDataToConfigMap(client corev1.ConfigMapsGetter, lister listerv1.ConfigMapLister, meta metav1.ObjectMeta, caBundle []byte) error {
	configmap, err := lister.ConfigMaps(meta.Namespace).Get(meta.Name)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("error when getting configmap %v: %v", meta.Name, err)
	}
	if errors.IsNotFound(err) {
		// Create a new ConfigMap.
		configmap = &v1.ConfigMap{
			ObjectMeta: meta,
			Data: map[string]string{
				constants.CACertNamespaceConfigMapDataName: string(caBundle),
			},
		}
		if _, err = client.ConfigMaps(meta.Namespace).Create(context.TODO(), configmap, metav1.CreateOptions{}); err != nil {
			// Namespace may be deleted between now... and our previous check. Just skip this, we cannot create into deleted ns
			// And don't retry a create if the namespace is terminating
			if errors.IsAlreadyExists(err) || errors.HasStatusCause(err, v1.NamespaceTerminatingCause) {
				return nil
			}
			return fmt.Errorf("error when creating configmap %v: %v", meta.Name, err)
		}
	} else {
		// Otherwise, update the config map if changes are required
		err := updateDataInConfigMap(client, configmap, caBundle)
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

func updateDataInConfigMap(client corev1.ConfigMapsGetter, cm *v1.ConfigMap, caBundle []byte) error {
	if cm == nil {
		return fmt.Errorf("cannot update nil configmap")
	}
	newCm := cm.DeepCopy()
	data := map[string]string{
		constants.CACertNamespaceConfigMapDataName: string(caBundle),
	}
	if needsUpdate := insertData(newCm, data); !needsUpdate {
		return nil
	}
	if _, err := client.ConfigMaps(newCm.Namespace).Update(context.TODO(), newCm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("error when updating configmap %v: %v", cm.Name, err)
	}
	return nil
}
