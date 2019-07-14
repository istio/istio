// Copyright 2019 Istio Authors
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

	"istio.io/pkg/log"

	configSchema "istio.io/istio/galley/pkg/config/schema"
	"istio.io/istio/galley/pkg/config/source/kube"

	"github.com/hashicorp/go-multierror"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
)

var scope = log.RegisterScope("source", "", 0)

var (
	pollInterval = time.Second
	pollTimeout  = time.Minute
)

// ResourceTypesPresence verifies that all expected k8s resources types are
// present in the k8s apiserver.
func ResourceTypesPresence(k kube.Interfaces, resources configSchema.KubeResources) error {
	cs, err := k.APIExtensionsClientset()
	if err != nil {
		return err
	}
	return resourceTypesPresence(cs, resources)
}

func resourceTypesPresence(cs clientset.Interface, resources configSchema.KubeResources) error {
	required := make(map[string]configSchema.KubeResource, len(resources))
	for _, spec := range resources {
		if !spec.Optional {
			required[spec.Plural] = spec
		}
	}

	err := wait.Poll(pollInterval, pollTimeout, func() (bool, error) {
		var errs error
	nextResource:
		for _, spec := range resources {
			if spec.Optional {
				continue
			}

			gv := schema.GroupVersion{Group: spec.Group, Version: spec.Version}.String()
			list, err := cs.Discovery().ServerResourcesForGroupVersion(gv)
			if err != nil {
				errs = multierror.Append(errs, fmt.Errorf("could not find %v: %v", gv, err))
				continue nextResource
			}
			found := false
			for _, r := range list.APIResources {
				if r.Name == spec.Plural {
					delete(required, spec.Plural)
					found = true
					break
				}
			}
			if !found {
				scope.Warnf("%s resource type not found", spec.CanonicalResourceName())
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
		scope.Errorf("Expected resources (CRDs) not found: %v", notFound)
		scope.Error("To stop Galley from waiting for these resources (CRDs), consider using the --excludedResourceKinds flag")

		return fmt.Errorf("%v: the following resource type(s) were not found: %v", err, notFound)
	}

	return nil
}
