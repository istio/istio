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
)

const (
	startSnippetLineFormat = "$snippet %s%s\n"
	endSnippetLine         = "$endsnippet\n"
	syntaxFormat           = " syntax=\"%s\""
	outputisFormat         = " outputis=\"%s\""
)

var _ Step = Snippet{}

// LineFilter allows applying a filter to the content generated in the snippets.
type LineFilter func(content string) (include bool, result string)

type Snippet struct {
	// Input chooses the input source to be used for the snippet.
	Input InputSelector

	// Name of the generated snippet file. If not provided, uses the same name as the File.
	Name string

	// Syntax for the snippet. Will not be included if not specified.
	Syntax string

	// OutputIs value for the snippet. Will not be included if not specified.
	OutputIs string
}

func (s Snippet) run(ctx Context) {
	ctx.Helper()

	input := s.Input.SelectInput(ctx)
	snippetName := s.getName(input)

	if snippetName == "" {
		ctx.Fatalf("snippet must be given a name")
	}

	content, err := input.ReadAll()
	if err != nil {
		ctx.Fatalf("failed writing snippet %s: %v", snippetName, err)
	}

	// Create the text metadata for the snippet, if provided.
	snippetMetadata := ""
	if s.Syntax != "" {
		snippetMetadata += fmt.Sprintf(syntaxFormat, s.Syntax)
	}
	if s.OutputIs != "" {
		snippetMetadata += fmt.Sprintf(outputisFormat, s.OutputIs)
	}

	// Start the snippet with the named snippet annotation.
	snippetContent := fmt.Sprintf(startSnippetLineFormat, snippetName, snippetMetadata)

	// Add the content
	snippetContent += content + "\n"

	// End the snippet
	snippetContent += endSnippetLine + "\n"

	// Write the snippet.
	if _, err := ctx.SnippetsFile.Write([]byte(snippetContent)); err != nil {
		ctx.Fatalf("failed writing snippet %s: %v", snippetName, err)
	}
}

func (s Snippet) getName(input Input) string {
	if s.Name != "" {
		return s.Name
	}
	return filepath.Base(input.Name())
}
