/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package rbac contain libraries for generating RBAC manifests from RBAC
// annotations in Go source files.
package rbac

import (
	"log"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-tools/pkg/internal/general"
)

type parserOptions struct {
	rules []rbacv1.PolicyRule
}

// parseAnnotation parses RBAC annotations
func (o *parserOptions) parseAnnotation(commentText string) error {
	for _, comment := range strings.Split(commentText, "\n") {
		comment := strings.TrimSpace(comment)
		if strings.HasPrefix(comment, "+rbac") {
			if ann := general.GetAnnotation(comment, "rbac"); ann != "" {
				o.rules = append(o.rules, parseRBACTag(ann))
			}
		}
		if strings.HasPrefix(comment, "+kubebuilder:rbac") {
			if ann := general.GetAnnotation(comment, "kubebuilder:rbac"); ann != "" {
				o.rules = append(o.rules, parseRBACTag(ann))
			}
		}
	}
	return nil
}

// parseRBACTag parses the given RBAC annotation in to an RBAC PolicyRule.
// This is copied from Kubebuilder code.
func parseRBACTag(tag string) rbacv1.PolicyRule {
	result := rbacv1.PolicyRule{}
	for _, elem := range strings.Split(tag, ",") {
		key, value, err := general.ParseKV(elem)
		if err != nil {
			log.Fatalf("// +kubebuilder:rbac: tags must be key value pairs.  Expected "+
				"keys [groups=<group1;group2>,resources=<resource1;resource2>,verbs=<verb1;verb2>] "+
				"Got string: [%s]", tag)
		}
		values := strings.Split(value, ";")
		switch key {
		case "groups":
			normalized := []string{}
			for _, v := range values {
				if v == "core" {
					normalized = append(normalized, "")
				} else {
					normalized = append(normalized, v)
				}
			}
			result.APIGroups = normalized
		case "resources":
			result.Resources = values
		case "verbs":
			result.Verbs = values
		case "urls":
			result.NonResourceURLs = values
		}
	}
	return result
}
