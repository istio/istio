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

package scopes

import "istio.io/istio/pkg/log"

var (
	// Framework is the general logging scope for the framework.
	Framework = log.RegisterScope("tf", "General scope for the test framework", 0)

	// Lab is the CI system specific logging scope.
	Lab = log.RegisterScope("lab", "Scope for normal log reporting to be used by the lab", 0)
)
