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

package kube

// Types in the schema.
var Types = Schema{}

func init() {
	// TODO: Full set of Schema.
	Types.add(Info{
		Kind:     "Rule",
		ListKind: "RuleList",
		Singular: "rule",
		Plural:   "rules",
		Version:  "v1beta1",
		Group:    "config.istio.io",
		Target:   getTargetFor("istio.policy.v1beta1.Rule"),
	})

	Types.add(Info{
		Kind:     "DestinationRule",
		ListKind: "DestinationRuleList",
		Singular: "destinationrule",
		Plural:   "destinationrules",
		Version:  "v1alpha3",
		Group:    "config.istio.io",
		Target:   getTargetFor("istio.networking.v1alpha3.DestinationRule"),
	})
}
