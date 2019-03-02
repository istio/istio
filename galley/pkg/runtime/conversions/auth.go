//  Copyright 2019 Istio Authors
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

package conversions

import (
	"fmt"
	"strings"

	"github.com/gogo/protobuf/proto"

	authn "istio.io/api/authentication/v1alpha1"
	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/resource"
)

// ServiceToSelector maps service to label selectors.
type ServiceToSelector struct {
	// Name maps the service name to its label selector. e.g. "httpbin": {"app": "httpbin", "version": "v1"}
	Name map[string]resource.Labels
}

// AuthConverter is used to retrieve label selector for a service with given name and namespace.
type AuthConverter struct {
	// Namespace maps the namespace name to the ServiceToSelector for the given namespace.
	Namespace map[string]ServiceToSelector
}

// AddService adds a service name and its associated selector labels to the AuthConverter.
func (ac *AuthConverter) AddService(fullName resource.FullName, selector resource.Labels) {
	if len(selector) == 0 {
		// Ignore empty selector for headless service.
		// TODO: Figure out what is the best way to handle this.
		return
	}

	// The Namespace could be nil for the first time calling AddService(). Create the map for later use.
	if ac.Namespace == nil {
		ac.Namespace = make(map[string]ServiceToSelector)
	}

	namespace, name := fullName.InterpretAsNamespaceAndName()
	serviceToSelector, ok := ac.Namespace[namespace]
	if !ok {
		ac.Namespace[namespace] = ServiceToSelector{Name: map[string]resource.Labels{name: selector}}
		return
	}

	serviceToSelector.Name[name] = selector
	return
}

// GetSelectors returns a list of selectors matched with the given service name and namespace.
// If allowPrefixSuffix is true, allows "*" to be used in the name for prefix/suffix match, otherwise
// use exact match and the returned list contains 1 element at most.
func (ac AuthConverter) GetSelectors(name, namespace string, allowPrefixSuffixName bool) []resource.Labels {
	ret := make([]resource.Labels, 0)
	serviceToSelector, ok := ac.Namespace[namespace]
	if !ok || len(serviceToSelector.Name) == 0 {
		return ret
	}

	matchFn := func(selectorName string) (matched, stop bool) {
		matched = name == selectorName
		// Return early as we should only have 1 match if we use exact match.
		stop = matched
		return
	}

	if allowPrefixSuffixName {
		if name == "*" {
			matchFn = func(_ string) (matched, stop bool) {
				return true, false
			}
		} else {
			checkPrefix := strings.HasSuffix(name, "*")
			checkSuffix := strings.HasPrefix(name, "*")

			if checkPrefix && checkSuffix {
				scope.Errorf("couldn't get selectors for invalid service name: %s", name)
				return ret
			} else if checkPrefix {
				name = strings.TrimSuffix(name, "*")
				matchFn = func(selectorName string) (matched, stop bool) {
					return strings.HasPrefix(selectorName, name), false
				}
			} else if checkSuffix {
				name = strings.TrimPrefix(name, "*")
				matchFn = func(selectorName string) (matched, stop bool) {
					return strings.HasSuffix(selectorName, name), false
				}
			}
		}
	}

	for k, v := range serviceToSelector.Name {
		matched, stop := matchFn(k)
		if matched {
			ret = append(ret, v)
		}
		if stop {
			break
		}
	}
	return ret
}

func ToAuthenticationPolicy(e *mcp.Resource) (*authn.Policy, error) {
	p := metadata.IstioAuthenticationV1alpha1Policies.NewProtoInstance()
	i, ok := p.(*authn.Policy)
	if !ok {
		// Shouldn't happen
		return nil, fmt.Errorf("unable to convert proto to AuthenticationPolicy: %v", p)
	}

	if err := proto.Unmarshal(e.Body.Value, p); err != nil {
		// Shouldn't happen
		return nil, fmt.Errorf("unable to unmarshal AuthenticationPolicy: %v", err)
	}

	return i, nil
}
