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

package features

// TODO: this file should be generated from YAML to make it more easy to modify.

// WARNING: changes to existing elements in this file will cause corruption of test coverage data.
// don't change existing entries unless absolutely necessary

type Feature string

const (
	UsabilityObservabilityStatus              Feature = "Usability.Observability.Status"
	UsabilityObservabilityStatusDefaultExists Feature = "Usability.Observability.Status.DefaultExists"
)
