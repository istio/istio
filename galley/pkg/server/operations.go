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

package server

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/galley/pkg/common"
	"istio.io/istio/galley/pkg/crd"
	"istio.io/istio/galley/pkg/resource"
	"istio.io/istio/pkg/log"
)

// Purge internal custom resources and custom resource definitions, based on the provided mapping.
func Purge(k common.Kube, mapping crd.Mapping) error {
	crdi, err := k.CustomResourceDefinitionInterface()
	if err != nil {
		return err
	}

	kubernetes, err := k.KubernetesInterface()
	if err != nil {
		return err
	}

	crds, err := crd.GetAll(crdi)
	if err != nil {
		return err
	}

	var nslist []string
	if nslist, err = resource.GetNamespaces(kubernetes); err != nil {
		return err
	}

	for _, c := range crds {

		var exists bool
		var destinationGv schema.GroupVersion
		if _, destinationGv, exists = mapping.GetGroupVersion(c.Spec.Group); !exists || c.Spec.Group != destinationGv.Group {
			continue
		}

		if e := resource.DeleteAll(
			k, c.Spec.Names.Plural, c.Spec.Names.Kind, c.Spec.Names.ListKind, destinationGv, nslist); e != nil {
			log.Errorf("Deletion error: name='%s', err:'%v'", c.Name, e)
			if err == nil {
				err = e
			}
		}
	}

	if err != nil {
		return fmt.Errorf("purge error: %v", err)
	}

	return crd.Purge(crdi, mapping)
}
