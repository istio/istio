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

package cluster

// Internal interface defines internal implementation details of the environment that is used by the
// test framework *only*. It should never be used directly in a test, or a test utility that resides with the
// tests themselves.
type Internal interface {
	// DoFoo is a dummy method to distinguish between clusters.
	DoFoo()
}
