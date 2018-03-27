//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package resource

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/log"
)

// DeleteAll deletes all resources in the specified custom resource set for given namespaces.
func DeleteAll(
	config *rest.Config, name string, kind string, gv schema.GroupVersion, namespaces []string) (err error) {

	cfg := *config
	cfg.GroupVersion = &gv
	cfg.APIPath = "/apis"
	cfg.ContentType = runtime.ContentTypeJSON

	var iface dynamic.Interface
	if iface, err = newDynamicClient(&cfg); err != nil {
		return err
	}

	apiResource := &metav1.APIResource{
		Name:       name,
		Group:      gv.Group,
		Version:    gv.Version,
		Namespaced: true,
		Kind:       kind,
	}

	for _, ns := range namespaces {
		log.Infof("Deleting all resources: name:%s (%s/%s), kind:%s, ns:%s",
			name, gv.Group, gv.Version, kind, ns)
		if err = iface.Resource(apiResource, ns).
			DeleteCollection(&metav1.DeleteOptions{}, metav1.ListOptions{}); err != nil {
			return err
		}
	}

	return nil
}

// GetNamespaces returns the currently known namespaces.
func GetNamespaces(config *rest.Config) ([]string, error) {

	k, err := newKubernetesClient(config)
	if err != nil {
		return nil, err
	}

	var namespaces []string
	continuation := ""

	for {
		var nslist *v1.NamespaceList
		if nslist, err = k.CoreV1().Namespaces().List(metav1.ListOptions{Continue: continuation}); err != nil {
			return nil, err
		}

		for _, ns := range nslist.Items {
			namespaces = append(namespaces, ns.Name)
		}

		continuation = nslist.Continue
		if continuation == "" {
			break
		}
	}

	return namespaces, nil
}
