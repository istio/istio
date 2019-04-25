// Copyright 2017 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"strings"
)

func (renderer *Renderer) RenderProvider() ([]byte, error) {
	f := NewLineWriter()
	f.WriteLine("// GENERATED FILE: DO NOT EDIT!\n")
	f.WriteLine("package " + renderer.Package)
	f.WriteLine(``)
	f.WriteLine(`// To create a server, first write a class that implements this interface.`)
	f.WriteLine(`// Then pass an instance of it to Initialize().`)
	f.WriteLine(`type Provider interface {`)
	for _, method := range renderer.Model.Methods {
		parametersType := renderer.Model.TypeWithTypeName(method.ParametersTypeName)
		responsesType := renderer.Model.TypeWithTypeName(method.ResponsesTypeName)
		f.WriteLine(``)
		f.WriteLine(commentForText(method.Description))
		if parametersType != nil {
			if responsesType != nil {
				f.WriteLine(method.ProcessorName +
					`(parameters *` + parametersType.Name +
					`, responses *` + responsesType.Name + `) (err error)`)
			} else {
				f.WriteLine(method.ProcessorName + `(parameters *` + parametersType.Name + `) (err error)`)
			}
		} else {
			if responsesType != nil {
				f.WriteLine(method.ProcessorName + `(responses *` + responsesType.Name + `) (err error)`)
			} else {
				f.WriteLine(method.ProcessorName + `() (err error)`)
			}
		}
	}
	f.WriteLine(`}`)
	return f.Bytes(), nil
}

func commentForText(text string) string {
	result := ""
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += "// " + line
	}
	return result
}
