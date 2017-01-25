// Copyright 2017 Google Inc.
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

// Basic template engine using go templates

package main

import (
	"io/ioutil"
	"log"
	"os"
	"text/template"

	yaml "gopkg.in/yaml.v2"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Must supply at least one template file")
	}

	bytes, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		log.Fatalf("Cannot read input %v", err)
	}

	out := make(map[interface{}]interface{})
	err = yaml.Unmarshal(bytes, &out)
	if err != nil {
		log.Fatalf("Cannot parse input %v", err)
	}

	for _, arg := range os.Args[1:] {
		tmpl, err := template.ParseFiles(arg)
		if err != nil {
			log.Fatalf("Template error %v", err)
		}
		if err := tmpl.Execute(os.Stdout, out); err != nil {
			log.Fatalf("Template execution error %v", err)
		}
	}
}
