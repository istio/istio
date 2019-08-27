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

// A simple program that consumes a YAML file describing Istio schemas and produces as output
// a Go source file providing variable definitions for those schemas.

package main

import (
	"bytes"
	"flag"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"
)

const (
	outputTemplate = `
// GENERATED FILE -- DO NOT EDIT

package {{ .Package }}

import (
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/validation"
)

var (
	{{ range .Schemas }}
{{ commentBlock (wordWrap (printf "%s %s" .VariableName .Description) 70) 2 }}
		{{ .VariableName }} = schema.Instance {
          Type: "{{ .Type }}",
          Plural: "{{ .Plural }}",
          Group: "{{ .Group }}",
          Version: "{{ .Version }}",
          MessageName: "{{ .MessageName }}",
          Validate: validation.{{ .Validate }},
          Collection: "{{ .Collection }}",
          ClusterScoped: {{ .ClusterScoped }},
          VariableName: "{{ .VariableName }}",
        }
	{{ end }}

    // Istio lists all Istio schemas.
    Istio = schema.Set{
    {{ range .Schemas -}}
      {{ .VariableName }},
    {{ end -}}
    }
)
`
)

var (
	input  string
	output string

	rootCmd = cobra.Command{
		Use:   "gen",
		Short: "Generates a Go source file containing Istio schemas.",
		Long:  "Generates a Go source file containing Istio schemas.",
		Run: func(cmd *cobra.Command, args []string) {
			yamlContent, err := ioutil.ReadFile(input)
			if err != nil {
				log.Fatalf("unable to read input file: %v", err)
			}

			// Unmarshal the file.
			var cfg Content
			if err = yaml.Unmarshal(yamlContent, &cfg); err != nil {
				log.Fatalf("error parsing input file: %v", err)
			}

			// Generate variable names if not provided in the yaml.
			for i := range cfg.Schemas {
				if cfg.Schemas[i].Type == "" {
					log.Fatalf("schema %d in input file missing type", i)
				}
				if cfg.Schemas[i].Plural == "" {
					log.Fatalf("schema %d in input file missing plural", i)
				}
				if cfg.Schemas[i].Group == "" {
					log.Fatalf("schema %d in input file missing group", i)
				}
				if cfg.Schemas[i].Version == "" {
					log.Fatalf("schema %d in input file missing version", i)
				}
				if cfg.Schemas[i].Collection == "" {
					log.Fatalf("schema %d in input file missing collection", i)
				}

				// Use defaults for variable name and validation function if not specified.
				if cfg.Schemas[i].VariableName == "" {
					cfg.Schemas[i].VariableName = variableNameForType(cfg.Schemas[i].Type)
				}
				if cfg.Schemas[i].Validate == "" {
					cfg.Schemas[i].Validate = "Validate" + cfg.Schemas[i].VariableName
				}
			}

			// Create the output file template.
			t, err := template.New("schemaTmpl").Funcs(template.FuncMap{
				"wordWrap":     wordWrap,
				"commentBlock": commentBlock,
			}).Parse(outputTemplate)
			if err != nil {
				log.Fatalf("failed parsing annotation template: %v", err)
			}

			// Generate the Go source.
			var goSource bytes.Buffer
			if err := t.Execute(&goSource, map[string]interface{}{
				"Package": getPackage(),
				"Schemas": cfg.Schemas,
			}); err != nil {
				log.Fatalf("failed generating output Go source code %s: %v", output, err)
			}

			if err := ioutil.WriteFile(output, goSource.Bytes(), 0666); err != nil {
				log.Fatalf("Failed writing to output file %s: %v", output, err)
			}
		},
	}
)

func init() {
	rootCmd.PersistentFlags().StringVar(&input, "input", "",
		"Input YAML file to be parsed.")
	rootCmd.PersistentFlags().StringVar(&output, "output", "",
		"Output Go file to be generated.")

	flag.CommandLine.VisitAll(func(gf *flag.Flag) {
		rootCmd.PersistentFlags().AddGoFlag(gf)
	})
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
}

type Content struct {
	Schemas []SchemaInfo `json:"schemas"`
}

// Instance describes a single resource annotation
type SchemaInfo struct {
	Type          string `json:"type"`
	Plural        string `json:"plural"`
	Group         string `json:"group"`
	Version       string `json:"version"`
	MessageName   string `json:"messageName"`
	Validate      string `json:"validate"`
	Collection    string `json:"collection"`
	ClusterScoped bool   `json:"clusterScoped"`
	VariableName  string `json:"variableName"`
	Description   string `json:"description"`
}

func getPackage() string {
	return filepath.Base(filepath.Dir(output))
}

func variableNameForType(typeName string) string {
	// Separate the words with spaces and capitalize each word.
	name := strings.ReplaceAll(typeName, "-", " ")
	name = strings.Title(name)

	// Remove the spaces to generate a camel case variable name.
	name = strings.ReplaceAll(name, " ", "")

	return name
}

func commentBlock(in []string, indentTabs int) string {
	prefix := ""
	for i := 0; i < indentTabs; i++ {
		prefix += "\t"
	}
	prefix += "// "

	// Apply the comment prefix to each line.
	in = prefixLines(in, prefix)

	// Join the lines with carriage returns.
	return strings.Join(in, "\n")
}

func prefixLines(in []string, prefix string) []string {
	out := make([]string, 0, len(in))
	for _, str := range in {
		out = append(out, prefix+str)
	}
	return out
}

func wordWrap(in string, maxLineLength int) []string {
	// First, split the input based on any user-created lines (i.e. the string contains "\n").
	inputLines := strings.Split(in, "\n")
	outputLines := make([]string, 0)

	line := ""
	for i, inputLine := range inputLines {
		if i > 0 {
			// Process a user-defined carriage return.
			outputLines = append(outputLines, line)
			line = ""
		}

		words := strings.Split(inputLine, " ")

		for len(words) > 0 {
			// Take the next word.
			word := words[0]
			words = words[1:]

			if len(line)+len(word) > maxLineLength {
				// Need to word wrap - emit the current line.
				outputLines = append(outputLines, line)
				line = ""
			}

			// Add the word to the current line.
			if len(line) > 0 {
				line += " "
			}
			line += word
		}
	}

	// Emit the final line
	outputLines = append(outputLines, line)

	return outputLines
}
