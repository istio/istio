// Copyright 2019 The Operator-SDK Authors
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

package scorecard

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/operator-framework/operator-sdk/internal/util/k8sutil"

	olmapiv1alpha1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func matchKind(kind1, kind2 string) bool {
	singularKind1, err := restMapper.ResourceSingularizer(kind1)
	if err != nil {
		singularKind1 = kind1
		log.Warningf("could not find singular version of %s", kind1)
	}
	singularKind2, err := restMapper.ResourceSingularizer(kind2)
	if err != nil {
		singularKind2 = kind2
		log.Warningf("could not find singular version of %s", kind2)
	}
	return strings.EqualFold(singularKind1, singularKind2)
}

// matchVersion checks if a CRD contains a specified version in a case insensitive manner
func matchVersion(version string, crd *apiextv1beta1.CustomResourceDefinition) bool {
	if strings.EqualFold(version, crd.Spec.Version) {
		return true
	}
	// crd.Spec.Version is deprecated, so check in crd.Spec.Versions as well
	for _, currVer := range crd.Spec.Versions {
		if strings.EqualFold(version, currVer.Name) {
			return true
		}
	}
	return false
}

// Run - implements Test interface
func (t *CRDsHaveValidationTest) Run(ctx context.Context) *TestResult {
	res := &TestResult{Test: t}
	crds, err := k8sutil.GetCRDs(t.CRDsDir)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Errorf("failed to get CRDs in %s directory: %v", t.CRDsDir, err))
		return res
	}
	err = t.Client.Get(ctx, types.NamespacedName{Namespace: t.CR.GetNamespace(), Name: t.CR.GetName()}, t.CR)
	if err != nil {
		res.Errors = append(res.Errors, err)
		return res
	}
	// TODO: we need to make this handle multiple CRs better/correctly
	for _, crd := range crds {
		res.MaximumPoints++
		if crd.Spec.Validation == nil {
			res.Suggestions = append(res.Suggestions, fmt.Sprintf("Add CRD validation for %s/%s", crd.Spec.Names.Kind, crd.Spec.Version))
			continue
		}
		// check if the CRD matches the testing CR
		gvk := t.CR.GroupVersionKind()
		// Only check the validation block if the CRD and CR have the same Kind and Version
		if !(matchVersion(gvk.Version, crd) && matchKind(gvk.Kind, crd.Spec.Names.Kind)) {
			res.EarnedPoints++
			continue
		}
		failed := false
		if t.CR.Object["spec"] != nil {
			spec := t.CR.Object["spec"].(map[string]interface{})
			for key := range spec {
				if _, ok := crd.Spec.Validation.OpenAPIV3Schema.Properties["spec"].Properties[key]; !ok {
					failed = true
					res.Suggestions = append(res.Suggestions, fmt.Sprintf("Add CRD validation for spec field `%s` in %s/%s", key, gvk.Kind, gvk.Version))
				}
			}
		}
		if t.CR.Object["status"] != nil {
			status := t.CR.Object["status"].(map[string]interface{})
			for key := range status {
				if _, ok := crd.Spec.Validation.OpenAPIV3Schema.Properties["status"].Properties[key]; !ok {
					failed = true
					res.Suggestions = append(res.Suggestions, fmt.Sprintf("Add CRD validation for status field `%s` in %s/%s", key, gvk.Kind, gvk.Version))
				}
			}
		}
		if !failed {
			res.EarnedPoints++
		}
	}
	return res
}

// Run - implements Test interface
func (t *CRDsHaveResourcesTest) Run(ctx context.Context) *TestResult {
	res := &TestResult{Test: t}
	for _, crd := range t.CSV.Spec.CustomResourceDefinitions.Owned {
		res.MaximumPoints++
		gvk := t.CR.GroupVersionKind()
		if strings.EqualFold(crd.Version, gvk.Version) && matchKind(gvk.Kind, crd.Kind) {
			resources, err := getUsedResources(t.ProxyPod)
			if err != nil {
				log.Warningf("getUsedResource failed: %v", err)
			}
			allResourcesListed := true
			for _, resource := range resources {
				foundResource := false
				for _, listedResource := range crd.Resources {
					if matchKind(resource.Kind, listedResource.Kind) && strings.EqualFold(resource.Version, listedResource.Version) {
						foundResource = true
					}
				}
				if foundResource == false {
					allResourcesListed = false
				}
			}
			if allResourcesListed {
				res.EarnedPoints++
			}
		} else {
			if len(crd.Resources) > 0 {
				res.EarnedPoints++
			}
		}
	}
	if res.EarnedPoints < res.MaximumPoints {
		res.Suggestions = append(res.Suggestions, "Add resources to owned CRDs")
	}
	return res
}

func getUsedResources(proxyPod *v1.Pod) ([]schema.GroupVersionKind, error) {
	logs, err := getProxyLogs(proxyPod)
	if err != nil {
		return nil, err
	}
	resources := map[schema.GroupVersionKind]bool{}
	for _, line := range strings.Split(logs, "\n") {
		logMap := make(map[string]interface{})
		err := json.Unmarshal([]byte(line), &logMap)
		if err != nil {
			// it is very common to get "unexpected end of JSON input", so we'll leave this at the debug level
			log.Debugf("could not unmarshal line: %v", err)
			continue
		}
		/*
			There are 6 formats a resource uri can have:
			Cluster-Scoped:
				Collection: /apis/GROUP/VERSION/KIND
				Individual: /apis/GROUP/VERSION/KIND/NAME
				Core:       /api/v1/KIND
			Namespaces:
				All Namespaces:          /apis/GROUP/VERSION/KIND (same as cluster collection)
				Collection in Namespace: /apis/GROUP/VERSION/namespaces/NAMESPACE/KIND
				Individual:              /apis/GROUP/VERSION/namespaces/NAMESPACE/KIND/NAME
				Core:                    /api/v1/namespaces/NAMESPACE/KIND

			These urls are also often appended with options, which are denoted by the '?' symbol
		*/
		if msg, ok := logMap["msg"].(string); !ok || msg != "Request Info" {
			continue
		}
		uri, ok := logMap["uri"].(string)
		if !ok {
			log.Warn("URI type is not string")
			continue
		}
		removedOptions := strings.Split(uri, "?")[0]
		splitURI := strings.Split(removedOptions, "/")
		// first string is empty string ""
		if len(splitURI) < 2 {
			log.Warnf("Invalid URI: \"%s\"", uri)
			continue
		}
		splitURI = splitURI[1:]
		switch len(splitURI) {
		case 3:
			if splitURI[0] == "api" {
				resources[schema.GroupVersionKind{Version: splitURI[1], Kind: splitURI[2]}] = true
				break
			} else if splitURI[0] == "apis" {
				// this situation happens when the client enumerates the available resources of the server
				// Example: "/apis/apps/v1?timeout=32s"
				break
			}
			log.Warnf("Invalid URI: \"%s\"", uri)
		case 4:
			if splitURI[0] == "apis" {
				resources[schema.GroupVersionKind{Group: splitURI[1], Version: splitURI[2], Kind: splitURI[3]}] = true
				break
			}
			log.Warnf("Invalid URI: \"%s\"", uri)
		case 5:
			if splitURI[0] == "api" {
				resources[schema.GroupVersionKind{Version: splitURI[1], Kind: splitURI[4]}] = true
				break
			} else if splitURI[0] == "apis" {
				resources[schema.GroupVersionKind{Group: splitURI[1], Version: splitURI[2], Kind: splitURI[3]}] = true
				break
			}
			log.Warnf("Invalid URI: \"%s\"", uri)
		case 6, 7:
			if splitURI[0] == "apis" {
				resources[schema.GroupVersionKind{Group: splitURI[1], Version: splitURI[2], Kind: splitURI[5]}] = true
				break
			}
			log.Warnf("Invalid URI: \"%s\"", uri)
		}
	}
	var resourcesArr []schema.GroupVersionKind
	for gvk := range resources {
		resourcesArr = append(resourcesArr, gvk)
	}
	return resourcesArr, nil
}

// Run - implements Test interface
func (t *AnnotationsContainExamplesTest) Run(ctx context.Context) *TestResult {
	res := &TestResult{Test: t, MaximumPoints: 1}
	if t.CSV.Annotations != nil && t.CSV.Annotations["alm-examples"] != "" {
		res.EarnedPoints = 1
	}
	if res.EarnedPoints == 0 {
		res.Suggestions = append(res.Suggestions, fmt.Sprintf("Add an alm-examples annotation to your CSV to pass the %s test", t.GetName()))
	}
	return res
}

// Run - implements Test interface
func (t *StatusDescriptorsTest) Run(ctx context.Context) *TestResult {
	res := &TestResult{Test: t}
	err := t.Client.Get(ctx, types.NamespacedName{Namespace: t.CR.GetNamespace(), Name: t.CR.GetName()}, t.CR)
	if err != nil {
		res.Errors = append(res.Errors, err)
		return res
	}
	if t.CR.Object["status"] == nil {
		return res
	}
	statusBlock := t.CR.Object["status"].(map[string]interface{})
	res.MaximumPoints = len(statusBlock)
	var crd *olmapiv1alpha1.CRDDescription
	for _, owned := range t.CSV.Spec.CustomResourceDefinitions.Owned {
		if owned.Kind == t.CR.GetKind() {
			crd = &owned
			break
		}
	}
	if crd == nil {
		return res
	}
	for key := range statusBlock {
		for _, statDesc := range crd.StatusDescriptors {
			if statDesc.Path == key {
				res.EarnedPoints++
				delete(statusBlock, key)
				break
			}
		}
	}
	for key := range statusBlock {
		res.Suggestions = append(res.Suggestions, "Add a status descriptor for "+key)
	}
	return res
}

// Run - implements Test interface
func (t *SpecDescriptorsTest) Run(ctx context.Context) *TestResult {
	res := &TestResult{Test: t}
	err := t.Client.Get(ctx, types.NamespacedName{Namespace: t.CR.GetNamespace(), Name: t.CR.GetName()}, t.CR)
	if err != nil {
		res.Errors = append(res.Errors, err)
		return res
	}
	if t.CR.Object["spec"] == nil {
		return res
	}
	specBlock := t.CR.Object["spec"].(map[string]interface{})
	res.MaximumPoints = len(specBlock)
	var crd *olmapiv1alpha1.CRDDescription
	for _, owned := range t.CSV.Spec.CustomResourceDefinitions.Owned {
		if owned.Kind == t.CR.GetKind() {
			crd = &owned
			break
		}
	}
	if crd == nil {
		return res
	}
	for key := range specBlock {
		for _, statDesc := range crd.SpecDescriptors {
			if statDesc.Path == key {
				res.EarnedPoints++
				delete(specBlock, key)
				break
			}
		}
	}
	for key := range specBlock {
		res.Suggestions = append(res.Suggestions, "Add a spec descriptor for "+key)
	}
	return res
}
