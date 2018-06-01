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
// TODO: consider using protoc-gen-validate annotations to improve doc generation

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/golang/protobuf/proto"
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

func main() {
	data, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		croak("Unable to read input proto: %v\n", err)
	}

	var request plugin.CodeGeneratorRequest
	if err = proto.Unmarshal(data, &request); err != nil {
		croak("Unable to parse input proto: %v\n", err)
	}

	mode := htmlPage
	genWarnings := true
	emitYAML := false
	camelCaseFields := true
	customStyleSheet := ""
	perFile := false

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
				croak("Unsupported output mode of '%s' specified\n", v)
			}
		} else if k == "warnings" {
			switch strings.ToLower(v) {
			case "true":
				genWarnings = true
			case "false":
				genWarnings = false
			default:
				croak("Unknown value '%s' for warnings\n", v)
			}
		} else if k == "emit_yaml" {
			switch strings.ToLower(v) {
			case "true":
				emitYAML = true
			case "false":
				emitYAML = false
			default:
				croak("Unknown value '%s' for emit_yaml\n", v)
			}
		} else if k == "camel_case_fields" {
			switch strings.ToLower(v) {
			case "true":
				camelCaseFields = true
			case "false":
				camelCaseFields = false
			default:
				croak("Unknown value '%s' for camel_case_fields\n", v)
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
				croak("Unknown value '%s' for per_file", v)
			}
		} else {
			croak("Unknown argument '%s' specified\n", k)
		}
	}

	m, err := newModel(&request, perFile)
	if err != nil {
		croak("Unable to create model: %v\n", err)
	}

	filesToGen := make(map[*fileDescriptor]bool)
	for _, fileName := range request.FileToGenerate {
		fd := m.allFilesByName[fileName]
		if fd == nil {
			croak("Unable to find %s", request.FileToGenerate)
		}
		filesToGen[fd] = true
	}

	g := newHTMLGenerator(m, mode, genWarnings, emitYAML, camelCaseFields, customStyleSheet, perFile)
	response := g.generateOutput(filesToGen)

	data, err = proto.Marshal(response)
	if err != nil {
		croak("Unable to serialize output proto: %v\n", err)
	}

	_, err = os.Stdout.Write(data)
	if err != nil {
		croak("Unable to write output proto: %v\n", err)
	}
}

func croak(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format, args...)
	os.Exit(1)
}
