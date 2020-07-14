// Copyright Istio Authors
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

package scope

import "istio.io/pkg/log"

var (
	// Analysis is a logging scope used by configuration analysis component.
	Analysis = log.RegisterScope("analysis", "Scope for configuration analysis runtime", 0)

	// Processing is a logging scope used by configuration processing pipeline.
	Processing = log.RegisterScope("processing", "Scope for configuration processing runtime", 0)

	// Source is a logging scope for config event sources.
	Source = log.RegisterScope("source", "Scope for configuration event sources", 0)
)
