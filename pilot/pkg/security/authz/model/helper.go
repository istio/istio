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

package model

import (
	"fmt"

	istio_rbac "istio.io/api/rbac/v1alpha1"
)

func permissionTag(tag string) string {
	return fmt.Sprintf("MethodFromPermission[%s]", tag)
}

func simplePermission(service string, tag string) Permission {
	return Permission{
		Services: []string{service},
		Methods:  []string{permissionTag(tag)},
	}
}

func principalTag(tag string) string {
	return fmt.Sprintf("UserFromPrincipal[%s]", tag)
}

func simplePrincipal(tag string) Principal {
	return Principal{
		User: principalTag(tag),
	}
}

func fullPermission(tag string) Permission {
	return Permission{
		Services:   []string{"svc-" + tag},
		Hosts:      []string{"host-" + tag},
		NotHosts:   []string{"not-host-" + tag},
		Paths:      []string{"paths-" + tag},
		NotPaths:   []string{"not-paths-" + tag},
		Methods:    []string{"methods-" + tag},
		NotMethods: []string{"not-methods-" + tag},
		Ports:      []int32{1},
		NotPorts:   []int32{2},
		Constraints: []KeyValues{
			{
				"constraint-" + tag: []string{"value1-" + tag, "value2-" + tag},
			},
		},
	}
}

func fullPrincipal(tag string) Principal {
	return Principal{
		User:          "user-" + tag,
		Names:         []string{"names-" + tag},
		NotNames:      []string{"not-names-" + tag},
		Group:         "group-" + tag,
		Groups:        []string{"groups-" + tag},
		NotGroups:     []string{"not-groups-" + tag},
		Namespaces:    []string{"namespaces-" + tag},
		NotNamespaces: []string{"not-namespaces-" + tag},
		IPs:           []string{"ips-" + tag},
		NotIPs:        []string{"not-ips-" + tag},
		Properties: []KeyValues{
			{
				"property-" + tag: []string{"value-" + tag},
			},
		},
	}
}

func fullRule(tag string) *istio_rbac.AccessRule {
	return &istio_rbac.AccessRule{
		Services:   []string{"svc-" + tag},
		Hosts:      []string{"host-" + tag},
		NotHosts:   []string{"not-host-" + tag},
		Paths:      []string{"paths-" + tag},
		NotPaths:   []string{"not-paths-" + tag},
		Methods:    []string{"methods-" + tag},
		NotMethods: []string{"not-methods-" + tag},
		Ports:      []int32{1},
		NotPorts:   []int32{2},
		Constraints: []*istio_rbac.AccessRule_Constraint{
			{
				Key:    "constraint-" + tag,
				Values: []string{"value1-" + tag, "value2-" + tag},
			},
		},
	}
}

func fullSubject(tag string) *istio_rbac.Subject {
	return &istio_rbac.Subject{
		User:          "user-" + tag,
		Names:         []string{"names-" + tag},
		NotNames:      []string{"not-names-" + tag},
		Group:         "group-" + tag,
		Groups:        []string{"groups-" + tag},
		NotGroups:     []string{"not-groups-" + tag},
		Namespaces:    []string{"namespaces-" + tag},
		NotNamespaces: []string{"not-namespaces-" + tag},
		Ips:           []string{"ips-" + tag},
		NotIps:        []string{"not-ips-" + tag},
		Properties: map[string]string{
			"property-" + tag: "value-" + tag,
		},
	}
}
