//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package tmpl

import (
	"bytes"
	tpl "text/template"
)

// Evaluate the given template using the provided data.
func Evaluate(template string, data interface{}) (string, error) {
	t := tpl.New("test template")

	t2, err := t.Parse(template)
	if err != nil {
		return "", err
	}

	var b bytes.Buffer
	if err = t2.Execute(&b, data); err != nil {
		return "", err
	}

	return b.String(), nil
}
