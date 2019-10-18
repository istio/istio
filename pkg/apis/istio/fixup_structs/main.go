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

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"unicode"
)

/*
protoc does not support generating interface{} types, hence we have the following marker types:

// GOTYPE: map[string]interface{}
message TypeMapStringInterface {}

The marker type is TypeMapStringInterface while the real type is map[string]interface{}.
The above declaration leads protoc to generate the following struct and associated methods:

// GOTYPE: map[string]interface{}
type TypeMapStringInterface struct {
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

The code below removes all the generated code for the marker types, and replaces any instances
of those types with the real types they represent.

To make the resulting fields anonymous, insert a _ after GOTYPE e.g. // GOTYPE: _ myAnonymousType

An additional mechanism for adding fields is to insert // GOFIELD: in a structure. This binary will simply remove the
// GOFIELD: and leave behind whatever is after it. The difference with this approach is that no additional Go struct
are generated for this type.

*/

const (
	goTypeToken  = "// GOTYPE:"
	goFieldToken = "// GOFIELD:"
)

var (
	// valuesNameMapping defines special mapping of fields names between proto and value.yaml.
	// some fields naming is not a valid field name in proto but used in values.yaml, eg. istio-ingressgateway.
	valuesNameMapping = map[string]string{
		"istioEgressgateway":  "istio-egressgateway",
		"istioIngressgateway": "istio-ingressgateway",
		"proxyInit":           "proxy_init",
		"istioCni":            "istio_cni",
	}
	// replaceMapping substitutes the value for the key in all files.
	replaceMapping = map[string]string{
		"github.com/gogo/protobuf/protobuf/google/protobuf": "github.com/gogo/protobuf/types",
		goFieldToken: "",
	}
)

func main() {
	var filePath string
	flag.StringVar(&filePath, "f", "", "path to input file")
	flag.Parse()

	if filePath == "" {
		fmt.Println("-f flag is required.")
		os.Exit(1)
	}

	lines, err := getFileLines(filePath)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	subs := make(map[string]string)
	anonymous := make(map[string]bool)

	var tmp, out []string

	i := 0
	for i < len(lines) {
		l := lines[i]

		switch {
		// Remove any generated code associated with the GOTYPE: decorated marker structs.
		case strings.Contains(l, goTypeToken):
			v := strings.ReplaceAll(l, goTypeToken, "")
			nl := lines[i+1]
			nlv := strings.Split(nl, " ")
			// We expect the format to be "type MarkerType struct {"
			if len(nlv) != 4 || nlv[0] != "type" || nlv[2] != "struct" || nlv[3] != "{" {
				fmt.Printf("Bad GOTYPE target: %s\n", nl)
				os.Exit(1)
			}
			// Subs maps a marker type to a real type.
			st := "*" + strings.TrimSpace(nlv[1])
			subs[st] = strings.TrimSpace(v)
			if strings.Contains(l, "_ ") {
				anonymous[st] = true
				subs[st] = strings.ReplaceAll(subs[st], "_ ", "")
			}
			for !strings.HasPrefix(lines[i], "var xxx_messageInfo_") {
				i++
			}
			tmp = append(tmp, lines[i+1])
			i += 2

		default:
			tmp = append(tmp, l)
			i++
		}
	}

	for _, l := range tmp {
		skip := false
		for k, v := range subs {
			if strings.Contains(l, k) {
				// Remove registration calls for marker structs.
				if strings.Contains(l, "proto.RegisterType") {
					skip = true
					break
				}
				// Replace all references with the real type.
				l = strings.ReplaceAll(l, k, v)
				if anonymous[k] && !strings.HasPrefix(l, "func") {
					l = removeName(l)
				}
			}
		}
		if !skip {
			out = append(out, l)
		}
	}

	out = patch(out)

	if strings.Contains(filePath, "values_types") {
		out = patchValues(out)
	}

	fmt.Printf("Writing to output file %s\n", filePath)
	if err := ioutil.WriteFile(filePath, []byte(strings.Join(out, "\n")), 0644); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// removeName removes the struct field name in a string containing a struct field name, type and tags.
func removeName(l string) string {
	var i1, i2 int
	// Preserve leading spaces and/or tabs.
	for i, c := range l {
		if unicode.IsLetter(c) {
			i1 = i
			break
		}
	}
	for i, c := range l[i1:] {
		if !unicode.IsLetter(c) {
			i2 = i
			break
		}
	}

	return l[:i1] + l[i2+2:]
}

// patch does arbitrary string substitution patching.
func patch(lines []string) (output []string) {
	for _, line := range lines {
		// patching naming issues
		for oldv, newv := range replaceMapping {
			line = strings.ReplaceAll(line, oldv, newv)
		}
		output = append(output, line)
	}
	return output
}

// patchValues is helper function to patch generated values_types.pb.go based on special mapping.
func patchValues(lines []string) (output []string) {
	for _, line := range lines {
		// patching naming issues
		for oldv, newv := range valuesNameMapping {
			line = strings.ReplaceAll(line, oldv, newv)
		}
		output = append(output, line)
	}
	return output
}

// getFileLines reads the text file at filePath and returns it as a slice of strings.
func getFileLines(filePath string) ([]string, error) {
	b, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	return strings.Split(string(b), "\n"), nil
}
