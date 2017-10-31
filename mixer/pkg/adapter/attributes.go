// Copyright 2017 Istio Authors
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

package adapter

type (
	// AttributesGenerator provides the interface for the Attributes aspect.
	// Implementors generate attribute values for consumption by other
	// aspects within Mixer.
	AttributesGenerator interface {
		Aspect

		// Generate takes a map of named input values and produces an
		// output map of named values. The input values will be
		// populated via configured attribute expressions. The output
		// map will be used to create new attribute values for use by
		// the rest of Mixer, as controlled by aspect config.
		Generate(map[string]interface{}) (map[string]interface{}, error)
	}

	// An AttributesGeneratorBuilder is responsible for producing new
	// instances that implement the AttributesGenerator aspect.
	AttributesGeneratorBuilder interface {
		Builder

		// BuildAttributesGenerator creates a new AttributesGenerator
		// based on the supplied configuration and environment.
		BuildAttributesGenerator(env Env, c Config) (AttributesGenerator, error)
	}
)
