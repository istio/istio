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

package resource

import (
	// Pull in all the known proto types to ensure we get their types registered.
	_ "istio.io/api/networking/v1alpha3"
	_ "istio.io/api/policy/v1beta1"
)

// Types of known resources.
var Types = NewSchema()

func init() {
	// TODO: Rest
	// Register known kinds here
	Types.Register("istio.policy.v1beta1.Rule")
	Types.Register("istio.networking.v1alpha3.DestinationRule")
}
