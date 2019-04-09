// Copyright 2018 Istio Authors
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

// TODO: Finish support for YAML output
// TODO: consider using protoc-gen-validate frontMatter to improve doc generation

package main

import (
	"fmt"
	"strings"

	"github.com/client9/gospell"

	"istio.io/tools/pkg/protocgen"

	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"
)

// Breaks the comma-separated list of key=value pairs
// in the parameter string into an easy to use map.
func extractParams(parameter string) map[string]string {
	m := make(map[string]string)
	for _, p := range strings.Split(parameter, ",") {
		if p == "" {
			continue
		}

		if i := strings.Index(p, "="); i < 0 {
			m[p] = ""
		} else {
			m[p[0:i]] = p[i+1:]
		}
	}

	return m
}

func generate(request plugin.CodeGeneratorRequest) (*plugin.CodeGeneratorResponse, error) {
	mode := htmlPage
	genWarnings := true
	emitYAML := false
	camelCaseFields := true
	customStyleSheet := ""
	perFile := false
	warningsAsErrors := false
	dictionary := ""
	customWordList := ""

	p := extractParams(request.GetParameter())
	for k, v := range p {
		if k == "mode" {
			switch strings.ToLower(v) {
			case "html_page":
				mode = htmlPage
			case "html_fragment":
				mode = htmlFragment
			case "jekyll_html":
				mode = htmlFragmentWithFrontMatter
			case "html_fragment_with_front_matter":
				mode = htmlFragmentWithFrontMatter
			default:
				return nil, fmt.Errorf("unsupported output mode of '%s' specified\n", v)
			}
		} else if k == "warnings" {
			switch strings.ToLower(v) {
			case "true":
				genWarnings = true
			case "false":
				genWarnings = false
			default:
				return nil, fmt.Errorf("unknown value '%s' for warnings\n", v)
			}
		} else if k == "emit_yaml" {
			switch strings.ToLower(v) {
			case "true":
				emitYAML = true
			case "false":
				emitYAML = false
			default:
				return nil, fmt.Errorf("unknown value '%s' for emit_yaml\n", v)
			}
		} else if k == "camel_case_fields" {
			switch strings.ToLower(v) {
			case "true":
				camelCaseFields = true
			case "false":
				camelCaseFields = false
			default:
				return nil, fmt.Errorf("unknown value '%s' for camel_case_fields\n", v)
			}
		} else if k == "custom_style_sheet" {
			customStyleSheet = v
		} else if k == "per_file" {
			switch strings.ToLower(v) {
			case "true":
				perFile = true
			case "false":
				perFile = false
			default:
				return nil, fmt.Errorf("unknown value '%s' for per_file\n", v)
			}
		} else if k == "warnings_as_errors" {
			switch strings.ToLower(v) {
			case "true":
				warningsAsErrors = true
			case "false":
				warningsAsErrors = false
			default:
				return nil, fmt.Errorf("unknown value '%s' for warnings_as_errors\n", v)
			}
		} else if k == "dictionary" {
			dictionary = v
		} else if k == "custom_word_list" {
			customWordList = v
		} else {
			return nil, fmt.Errorf("unknown argument '%s' specified\n", k)
		}
	}

	m, err := newModel(&request, perFile)
	if err != nil {
		return nil, fmt.Errorf("unable to create model: %v\n", err)
	}

	filesToGen := make(map[*fileDescriptor]bool)
	for _, fileName := range request.FileToGenerate {
		fd := m.allFilesByName[fileName]
		if fd == nil {
			return nil, fmt.Errorf("unable to find %s", request.FileToGenerate)
		}
		filesToGen[fd] = true
	}

	var s *gospell.GoSpell

	if dictionary != "" {
		s, err = gospell.NewGoSpell(dictionary+".aff", dictionary+".dic")
		if err != nil {
			return nil, fmt.Errorf("unable to load dictionary: %v\n", err)
		}

		if customWordList != "" {
			_, err = s.AddWordListFile(customWordList)
			if err != nil {
				return nil, fmt.Errorf("unable to load custom word list: %v\n", err)
			}
		}
	}

	g := newHTMLGenerator(m, mode, genWarnings, warningsAsErrors, s, emitYAML, camelCaseFields, customStyleSheet, perFile)
	return g.generateOutput(filesToGen)
}

func main() {
	protocgen.Generate(generate)
}
