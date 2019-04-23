// Copyright 2018 Istio Authors
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

package check

import (
	"fmt"
	"time"

	"istio.io/istio/galley/pkg/source/kube/client"
	"istio.io/istio/galley/pkg/source/kube/log"
	sourceSchema "istio.io/istio/galley/pkg/source/kube/schema"

	multierror "github.com/hashicorp/go-multierror"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	pollInterval = time.Second
	pollTimeout  = time.Minute
)

// ResourceTypesPresence verifies that all expected k8s resources types are
// present in the k8s apiserver.
func ResourceTypesPresence(k client.Interfaces, specs []sourceSchema.ResourceSpec) ([]sourceSchema.ResourceSpec, error) {
	cs, err := k.APIExtensionsClientset()
	if err != nil {
		return nil, err
	}
	return resourceTypesPresence(cs, specs, true)
}

// FindSupportedResourceSchemas returns the list of supported resource schemas supported by the k8s apiserver.
func FindSupportedResourceSchemas(k client.Interfaces, specs []sourceSchema.ResourceSpec) ([]sourceSchema.ResourceSpec, error) {
	cs, err := k.APIExtensionsClientset()
	if err != nil {
		return nil, err
	}
	return resourceTypesPresence(cs, specs, false)
}

func resourceTypesPresence(cs clientset.Interface, specs []sourceSchema.ResourceSpec, pollRequired bool) ([]sourceSchema.ResourceSpec, error) {
	search := make(map[string]*sourceSchema.ResourceSpec, len(specs))
	required := make(map[string]*sourceSchema.ResourceSpec, len(specs))
	for i, spec := range specs {
		search[spec.Plural] = &specs[i]
		if pollRequired && !spec.Optional {
			required[spec.Plural] = &specs[i]
		}
	}

	err := wait.Poll(pollInterval, pollTimeout, func() (bool, error) {
		var errs error
	nextResource:
		for plural, spec := range search {
			gv := schema.GroupVersion{Group: spec.Group, Version: spec.Version}.String()
			list, err := cs.Discovery().ServerResourcesForGroupVersion(gv)
			if err != nil {
				errs = multierror.Append(errs, fmt.Errorf("could not find %v: %v", gv, err))
				continue nextResource
			}
			found := false
			for _, r := range list.APIResources {
				if r.Name == spec.Plural {
					delete(search, plural)
					delete(required, plural)
					found = true
					break
				}
			}
			if !found {
				log.Scope.Warnf("%s resource type not found", spec.CanonicalResourceName())
			}
		}
		if len(required) == 0 {
			return true, nil
		}
		// entire search failed
		if errs != nil {
			return true, errs
		}
		// check again next poll
		return false, nil
	})

	if err != nil {
		var notFound []string
		for _, spec := range required {
			notFound = append(notFound, spec.Kind)
		}
		log.Scope.Errorf("Expected resources (CRDs) not found: %v", notFound)
		log.Scope.Error("To stop Galley from waiting for these resources (CRDs), consider using the --excludedResourceKinds flag")

		return nil, fmt.Errorf("%v: the following resource type(s) were not found: %v", err, notFound)
	}

	found := make([]sourceSchema.ResourceSpec, 0, len(specs)-len(search))
	for _, spec := range specs {
		if _, missing := search[spec.Plural]; !missing {
			found = append(found, spec)
		}
	}
	log.Scope.Infof("Discovered all supported resources (# = %v)", len(found))
	return found, nil
}
