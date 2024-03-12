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
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/log"
)

// InsertDataToConfigMap inserts a data to a configmap in a namespace.
// client: the k8s client interface.
// lister: the configmap lister.
// meta: the metadata of configmap.
// caBundle: ca cert data bytes.
func InsertDataToConfigMap(client kclient.Client[*v1.ConfigMap], meta metav1.ObjectMeta, caBundle []byte) error {
	configmap := client.Get(meta.Name, meta.Namespace)
	if configmap == nil {
		// Create a new ConfigMap.
		configmap = &v1.ConfigMap{
			ObjectMeta: meta,
			Data: map[string]string{
				constants.CACertNamespaceConfigMapDataName: string(caBundle),
			},
		}
		if _, err := client.Create(configmap); err != nil {
			// Namespace may be deleted between now... and our previous check. Just skip this, we cannot create into deleted ns
			// And don't retry a create if the namespace is terminating
			if errors.IsAlreadyExists(err) || errors.HasStatusCause(err, v1.NamespaceTerminatingCause) {
				return nil
			}
			if errors.IsForbidden(err) {
				log.Infof("skip writing ConfigMap %v/%v as we do not have permissions to do so", meta.Namespace, meta.Name)
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

func updateDataInConfigMap(c kclient.Client[*v1.ConfigMap], cm *v1.ConfigMap, caBundle []byte) error {
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
	if _, err := c.Update(newCm); err != nil {
		return fmt.Errorf("error when updating configmap %v: %v", cm.Name, err)
	}
	return nil
}
