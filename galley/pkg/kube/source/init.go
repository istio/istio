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
	"io/ioutil"
	"fmt"
	"os"
	"time"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/hashicorp/go-multierror"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"istio.io/istio/galley/pkg/kube"
	kube_meta "istio.io/istio/galley/pkg/metadata/kube"
)

func writeCRDs(cs clientset.Interface, fileName string) (error) {
	CRDData, err := ioutil.ReadFile(fileName)
	if err != nil {
		scope.Infof("Read CRD %v", err)
		return err
	}

	// yaml.YAMLtoJSON does not appear to understand "---".
	yamlStrings := strings.Split(string(CRDData), "---")
	// Build a set of yaml strings and range over them.
	for _, yamlStringCRD := range yamlStrings {
		var yamlCRD apiextensionsv1beta1.CustomResourceDefinition
		yaml.Unmarshal([]byte(yamlStringCRD), &yamlCRD)

		_, err = cs.ApiextensionsV1beta1().CustomResourceDefinitions().Create(&yamlCRD)
		if err != nil {
			scope.Infof("Error writing CRDs: %v", err)
			continue
//			return err
		}
	}
	return nil
}

func createResourceTypes(cs clientset.Interface, CRDDirectory string) (error) {
	CRDDirectoryFile, err := os.Open(CRDDirectory)
	if err != nil {
		return err
	}
	// Read all files into a FileInfo typed array
	fileInfo, err := CRDDirectoryFile.Readdir(0)
	if err != nil {
		return err
	}
	for _, file := range fileInfo {
		scope.Infof("filep %s", file.Name())
		if strings.HasSuffix(file.Name(), ".yaml") == false {
			continue
		}
		scope.Infof("opened configmap %s", file.Name())
		err = writeCRDs(cs, CRDDirectory + "/" + file.Name())
		if err != nil {
			scope.Infof("Error writing CRDs: %s", file.Name())
			continue
		}
		scope.Infof("Wrote CRDS from configmap %s", file.Name())
	}
	defer CRDDirectoryFile.Close() // nolint: errcheck

	return nil
}

func CreateResourceTypes(k kube.Interfaces) error {
	cs, err := k.APIExtensionsClientset()
	if err != nil {
		return err
	}
	createResourceTypes(cs, "/etc/istio/crds")
	if err != nil {
		return err
	}
	return nil
}

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
	pollInterval = time.Second
	pollTimeout  = time.Minute
)

func verifyResourceTypesPresence(cs clientset.Interface, specs []kube.ResourceSpec) error {
	search := make(map[string]*kube.ResourceSpec, len(specs))
	for i, spec := range specs {
		search[spec.Plural] = &specs[i]
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
