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

package istioio

import (
	"fmt"
	"path/filepath"

	"istio.io/istio/pkg/test/util/yml"
)

var _ Step = YamlResources{}

// YamlResources is a test step that generates individual snippets for resources within a given
// YAML document.
type YamlResources struct {
	// BaseName for the generated snippets. If not provided, the base name of Input will be used.
	BaseName string

	// Input YAML document.
	Input InputSelector

	// ResourceNames provides the names of the resources for which to generate snippets.
	ResourceNames []string
}

func (s YamlResources) run(ctx Context) {
	input := s.Input.SelectInput(ctx)

	content, err := input.ReadAll()
	if err != nil {
		ctx.Fatalf("failed reading YAML input %s: %v", input.Name(), err)
	}

	parts, err := yml.Parse(content)
	if err != nil {
		ctx.Fatalf("failed parsing YAML input %s: %v", input.Name(), err)
	}

	// Get the base name for the snippets.
	baseName := s.BaseName
	if baseName == "" {
		baseName = filepath.Base(input.Name())
	}

	for _, resourceName := range s.ResourceNames {
		found := false
		snippetName := fmt.Sprintf("%s_%s.yaml", baseName, resourceName)

		for _, part := range parts {
			if part.Descriptor.Metadata.Name == resourceName {
				found = true

				// Generate a snippet for this resource.
				Snippet{
					Name:   snippetName,
					Syntax: "yaml",
					Input: Inline{
						FileName: snippetName,
						Value:    part.Contents,
					},
				}.run(ctx)
				break
			}
		}

		if !found {
			ctx.Fatalf("failed to find YAML resource %s from input %s", resourceName, input.Name)
		}
	}
}
