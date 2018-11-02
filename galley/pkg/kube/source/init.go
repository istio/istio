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

package source

import (
	"fmt"
	"time"

	"github.com/hashicorp/go-multierror"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"

	"istio.io/istio/galley/pkg/kube"
	kube_meta "istio.io/istio/galley/pkg/metadata/kube"
)

// VerifyResourceTypesPresence verifies that all expected k8s resources types are
// present in the k8s apiserver.
func VerifyResourceTypesPresence(k kube.Interfaces) error {
	cs, err := k.APIExtensionsClientset()
	if err != nil {
		return err
	}
	return verifyResourceTypesPresence(cs, kube_meta.Types.All())
}

var (
	crdPresencePollInterval = time.Second
	crdPresensePollTimeout  = time.Minute
)

func verifyResourceTypesPresence(cs clientset.Interface, specs []kube.ResourceSpec) error {
	search := make(map[string]*kube.ResourceSpec, len(specs))
	for i, spec := range specs {
		search[spec.Plural] = &specs[i]
	}

	err := wait.Poll(crdPresencePollInterval, crdPresensePollTimeout, func() (bool, error) {
		var errs error
	nextCRD:
		for plural, spec := range search {
			gv := schema.GroupVersion{Group: spec.Group, Version: spec.Version}.String()
			list, err := cs.Discovery().ServerResourcesForGroupVersion(gv)
			if err != nil {
				errs = multierror.Append(errs, fmt.Errorf("could not find %v: %v", gv, err))
				continue nextCRD
			}
			found := false
			for _, r := range list.APIResources {
				if r.Name == spec.Plural {
					delete(search, plural)
					found = true
					break
				}
			}
			if !found {
				scope.Infof("%s resource type not found", spec.CanonicalResourceName())
			}
		}
		if len(search) == 0 {
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
		for plural := range search {
			notFound = append(notFound, plural)
		}
		return fmt.Errorf("%v: the following resource type(s) were not found: %v", err, notFound)
	}

	scope.Infof("Discovered all supported resources (# = %v)", len(specs))
	return nil
}
