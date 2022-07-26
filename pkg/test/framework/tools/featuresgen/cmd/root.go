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

package cmd

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

const (
	Copyright = `// Copyright Istio Authors
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

`

	GeneratedWarning = `
//WARNING: THIS IS AN AUTO-GENERATED FILE, DO NOT EDIT.
`

	TypeDefFeature = `
type Feature string
`

	Package = `
package features
`

	ConstOpen = `
const (
`

	ConstClose = `
)
`
)

var (
	input  string
	output string

	rootCmd = &cobra.Command{
		Use:   "featuresgen [OPTIONS]",
		Short: "FeaturesGen generates valid test labels from a yaml file",
		Run: func(cmd *cobra.Command, args []string) {
			createLabelsFile()
		},
	}

	alphanumericRegex = regexp.MustCompile(`[^a-zA-Z0-9-]`)
	dotsRegex         = regexp.MustCompile(`[\.]`)
	replaceDashRegex  = regexp.MustCompile(`-(.)`)
)

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println("Error running featuresgen:")
		panic(err)
	}
}

func init() {
	rootCmd.Flags().StringVarP(&input, "inputFile", "i", "features.yaml", "the YAML file to use as input")
	rootCmd.Flags().StringVarP(&output, "outputFile", "o", "features.gen.go", "output Go file with labels as string consts")
}

// Parses a map in the yaml file
func readMap(m map[any]any, path []string) []string {
	var labels []string
	for k, v := range m {
		// If we see "values," then the element is a root and we shouldn't put it in our label name
		if k == "values" {
			labels = append(labels, readVal(v, path)...)
		} else {
			if len(path) > 0 || k.(string) != "features" {
				path = append(path, k.(string))
			}
			labels = append(labels, readVal(v, path)...)
			if len(path) > 0 {
				path = path[:len(path)-1]
			}
		}
	}
	return labels
}

// Parses a slice in the yaml file
func readSlice(slc []any, path []string) []string {
	labels := make([]string, 0)
	for _, v := range slc {
		labels = append(labels, readVal(v, path)...)
	}
	return labels
}

// Determines the type of a node in the yaml file and parses it accordingly
func readVal(v any, path []string) []string {
	typ := reflect.TypeOf(v).Kind()
	if typ == reflect.Int || typ == reflect.String {
		path = append(path, v.(string))
		return []string{createConstantString(path)}
	} else if typ == reflect.Slice {
		return readSlice(v.([]any), path)
	} else if typ == reflect.Map {
		return readMap(v.(map[any]any), path)
	}
	panic("Found invalid type in YAML file")
}

func removeDashAndTitle(s string) string {
	// nolint: staticcheck
	return strings.Title(s[1:])
}

// Writes a label to the constants file
func createConstantString(path []string) string {
	name := ""
	value := ""
	for i := 0; i < len(path); i++ {
		namePart := alphanumericRegex.ReplaceAllString(path[i], "")
		namePart = replaceDashRegex.ReplaceAllStringFunc(namePart, removeDashAndTitle)
		// nolint: staticcheck
		namePart = strings.Title(namePart)
		name += namePart
		name += "_"

		value += dotsRegex.ReplaceAllString(path[i], "")
		value += "."
	}
	name = strings.TrimSuffix(name, "_")
	value = strings.TrimSuffix(value, ".")
	return fmt.Sprintf("\t%s\tFeature = \"%s\"", name, value)
}

// Reads the yaml file and generates a string constant for each leaf node
func createLabelsFromYaml() string {
	data, err := os.ReadFile(input)
	if err != nil {
		pwd, _ := os.Getwd()
		fmt.Println("Error running featuresgen on file: ", pwd, "/", input)
		panic(err)
	}
	m := make(map[any]any)

	err = yaml.Unmarshal(data, &m)
	if err != nil {
		pwd, _ := os.Getwd()
		fmt.Println("Error running featuresgen on file: ", pwd, "/", input)
		panic(err)
	}

	constants := readVal(m, make([]string, 0))
	// The parsing of the yaml file doesn't seem to happen in a consistent order. To avoid a different file every time 'make gen' is run, we sort the list.
	sort.Strings(constants)
	return strings.Join(constants, "\n")
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

// Main function that writes the new generated labels file
func createLabelsFile() {
	f, err := os.Create("./" + output)
	if err != nil {
		fmt.Println("Error running featuresgen:")
		panic(err)
	}

	defer f.Close()

	_, err = f.WriteString(Copyright)
	check(err)
	_, err = f.WriteString(GeneratedWarning)
	check(err)
	_, err = f.WriteString(Package)
	check(err)
	_, err = f.WriteString(TypeDefFeature)
	check(err)
	_, err = f.WriteString(ConstOpen)
	check(err)
	_, err = f.WriteString(createLabelsFromYaml())
	check(err)
	_, err = f.WriteString(ConstClose)
	check(err)
	err = f.Sync()
	check(err)
}
