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

package config

// Factory for config Plan resources.
type Factory interface {
	// New return an empty config Plan.
	New() Plan

	// YAML returns a Plan with the given YAML and target namespace.
	YAML(ns string, yamlText ...string) Plan

	// File reads the given files and calls YAML.
	File(ns string, paths ...string) Plan

	// Eval the same as YAML, but it evaluates the template parameters.
	Eval(ns string, args any, yamlText ...string) Plan

	// EvalFile the same as File, but it evaluates the template parameters.
	EvalFile(ns string, args any, paths ...string) Plan
}
