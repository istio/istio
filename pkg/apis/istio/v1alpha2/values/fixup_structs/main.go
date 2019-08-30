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
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
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
*/

const (
	goTypeToken = "// GOTYPE: "
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

	var tmp, out []string

	i := 0
	for i < len(lines) {
		l := lines[i]
		// Remove any generated code associated with the GOTYPE: decorated marker structs.
		if strings.Contains(l, goTypeToken) {
			v := strings.ReplaceAll(l, goTypeToken, "")
			nl := lines[i+1]
			nlv := strings.Split(nl, " ")
			// We expect the format to be "type MarkerType struct {"
			if len(nlv) != 4 || nlv[0] != "type" || nlv[2] != "struct" || nlv[3] != "{" {
				fmt.Printf("Bad GOTYPE target: %s\n", nl)
				os.Exit(1)
			}
			// Subs maps a marker type to a real type.
			subs["*"+strings.TrimSpace(nlv[1])] = strings.TrimSpace(v)
			for !strings.HasPrefix(lines[i], "var xxx_messageInfo_") {
				i++
			}
			i++
			l = lines[i]
		}

		tmp = append(tmp, l)
		i++
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
			}
		}
		if !skip {
			out = append(out, l)
		}
	}

	if err := ioutil.WriteFile(filePath, []byte(strings.Join(out, "\n")), 0644); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

}

// getFileLines reads the text file at filePath and returns it as a slice of strings.
func getFileLines(filePath string) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer file.Close()

	var out []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		out = append(out, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return out, nil
}
