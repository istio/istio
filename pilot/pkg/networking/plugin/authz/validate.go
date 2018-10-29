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

package authz

import (
	"fmt"
	"strings"

	rbacproto "istio.io/api/rbac/v1alpha1"
)

// validateRuleForTCPFilter checks if the given rule is good for RBAC TCP filter. Returns nil if the
// rule doesn't include any HTTP specifications.
func validateRuleForTCPFilter(rule *rbacproto.AccessRule) error {
	if rule == nil {
		return nil
	}

	if len(rule.Paths) != 0 {
		return fmt.Errorf("paths(%v) not supported", rule.Paths)
	}
	if len(rule.Methods) != 0 {
		return fmt.Errorf("methods(%v) not supported", rule.Methods)
	}
	for _, constraint := range rule.Constraints {
		if strings.HasPrefix(constraint.Key, attrRequestHeader) {
			return fmt.Errorf("constraint(%v) not supported", constraint)
		}
	}

	return nil
}

// validateBindingsForTCPFilter checks if the give binding is good for RBAC TCP filter. Returns nil
// if the binding doesn't include any HTTP specifications.
func validateBindingsForTCPFilter(bindings []*rbacproto.ServiceRoleBinding) error {
	for _, binding := range bindings {
		for _, subject := range binding.Subjects {
			if subject.Group != "" {
				return fmt.Errorf("group(%v) not supported", subject.Group)
			}
			for k := range subject.Properties {
				switch k {
				case attrSrcIP, attrSrcNamespace, attrSrcPrincipal:
					continue
				default:
					return fmt.Errorf("property(%v) not supported", k)
				}
			}
		}
	}
	return nil
}
