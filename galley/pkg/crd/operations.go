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

package crd

import (
	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/log"
)

// Get returns all known CRDs
func Get(config *rest.Config) ([]apiext.CustomResourceDefinition, error) {

	var err error
	var crdi v1beta1.CustomResourceDefinitionInterface
	if crdi, err = getCustomResourceDefinitionsInterface(config); err != nil {
		return nil, err
	}

	var crds []apiext.CustomResourceDefinition
	continuation := ""
	for {
		var list *apiext.CustomResourceDefinitionList
		list, err = crdi.List(v1.ListOptions{Continue: continuation})
		if err != nil {
			return nil, err
		}

		crds = append(crds, list.Items...)
		continuation = list.Continue

		if continuation == "" {
			break
		}
	}

	return crds, nil
}

// Purge deletes the destination CRDs as a synchronous operation.
func Purge(config *rest.Config, mapping Mapping) error {

	var err error
	var crdi v1beta1.CustomResourceDefinitionInterface
	if crdi, err = getCustomResourceDefinitionsInterface(config); err != nil {
		return err
	}

	var crds []apiext.CustomResourceDefinition
	continuation := ""
	for {
		var list *apiext.CustomResourceDefinitionList
		list, err = crdi.List(v1.ListOptions{Continue: continuation})
		if err != nil {
			return err
		}
		crds = append(crds, list.Items...)
		continuation = list.Continue

		if continuation == "" {
			break
		}
	}

	for _, crd := range crds {
		if _, destination, found := mapping.GetGroupVersion(crd.Spec.Group); found && destination.Group == crd.Spec.Group {
			log.Infof("Deleting crd: %s (%s/%s)", crd.Name, crd.Spec.Group, crd.Spec.Version)
			if err = crdi.Delete(crd.Name, &v1.DeleteOptions{}); err != nil {
				return err
			}
		}
	}

	return nil
}
