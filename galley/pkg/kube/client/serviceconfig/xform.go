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

package serviceconfig

import (
	"istio.io/istio/galley/pkg/api"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func FromUnstructured(u *unstructured.Unstructured) *api.ServiceConfig {
	spec := u.Object["spec"].(map[string]interface{})
	instances := convertInstanceDecls(spec["instances"])
	rules := convertRules(spec["rules"])

	s := &api.ServiceConfig{
		Name:      u.GetName(),
		Instances: instances,
		Rules:     rules,
	}

	return s
}

func convertInstanceDecls(iface interface{}) []*api.InstanceDecl {
	if iface == nil {
		return nil
	}

	a := iface.([]interface{})
	r := make([]*api.InstanceDecl, len(a))
	for i, in := range iface.([]interface{}) {
		r[i] = convertInstanceDecl(in)
	}
	return r
}

func convertInstanceDecl(iface interface{}) *api.InstanceDecl {
	m := iface.(map[string]interface{})

	// TODO: params
	return &api.InstanceDecl{
		Name:     convertString(m["name"]),
		Template: convertString(m["template"]),
	}
}

func convertRules(iface interface{}) []*api.Rule {
	if iface == nil {
		return nil
	}

	a := iface.([]interface{})
	r := make([]*api.Rule, len(a))
	for i, in := range iface.([]interface{}) {
		r[i] = convertRule(in)
	}
	return r
}

func convertRule(iface interface{}) *api.Rule {
	if iface == nil {
		return nil
	}

	m := iface.(map[string]interface{})

	// TODO: params
	return &api.Rule{
		Match:   convertString(m["match"]),
		Actions: convertActions(m["actions"]),
	}
}

func convertActions(iface interface{}) []*api.Action {
	if iface == nil {
		return nil
	}

	a := iface.([]interface{})
	r := make([]*api.Action, len(a))
	for i, in := range iface.([]interface{}) {
		r[i] = convertAction(in)
	}
	return r
}

func convertAction(iface interface{}) *api.Action {
	if iface == nil {
		return nil
	}

	m := iface.(map[string]interface{})

	return &api.Action{
		Handler:   convertString(m["handler"]),
		Instances: convertInstances(m["instances"]),
	}
}

func convertInstances(iface interface{}) []*api.Instance {
	if iface == nil {
		return nil
	}

	a := iface.([]interface{})
	r := make([]*api.Instance, len(a))
	for i, in := range iface.([]interface{}) {
		r[i] = convertInstance(in)
	}
	return r
}

func convertInstance(iface interface{}) *api.Instance {
	if iface == nil {
		return nil
	}

	m := iface.(map[string]interface{})

	// TODO: param
	return &api.Instance{
		Ref:      convertString(m["ref"]),
		Template: convertString(m["template"]),
	}
}

func convertString(iface interface{}) string {
	if iface == nil {
		return ""
	}
	return iface.(string)
}
