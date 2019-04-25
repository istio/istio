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

	surface "github.com/googleapis/gnostic/surface"
)

// ParameterList returns a string representation of a method's parameters
func ParameterList(parametersType *surface.Type) string {
	result := ""
	if parametersType != nil {
		for _, field := range parametersType.Fields {
			result += field.ParameterName + " " + field.NativeType + "," + "\n"
		}
	}
	return result
}

func (renderer *Renderer) RenderClient() ([]byte, error) {
	f := NewLineWriter()

	f.WriteLine("// GENERATED FILE: DO NOT EDIT!")
	f.WriteLine(``)
	f.WriteLine("package " + renderer.Package)

	// imports will be automatically added by goimports

	f.WriteLine(`// Client represents an API client.`)
	f.WriteLine(`type Client struct {`)
	f.WriteLine(`  service string`)
	f.WriteLine(`  APIKey string`)
	f.WriteLine(`  client *http.Client`)
	f.WriteLine(`}`)

	f.WriteLine(`// NewClient creates an API client.`)
	f.WriteLine(`func NewClient(service string, c *http.Client) *Client {`)
	f.WriteLine(`	client := &Client{}`)
	f.WriteLine(`	client.service = service`)
	f.WriteLine(`  if c != nil {`)
	f.WriteLine(`    client.client = c`)
	f.WriteLine(`  } else {`)
	f.WriteLine(`    client.client = http.DefaultClient`)
	f.WriteLine(`  }`)
	f.WriteLine(`	return client`)
	f.WriteLine(`}`)

	for _, method := range renderer.Model.Methods {
		parametersType := renderer.Model.TypeWithTypeName(method.ParametersTypeName)
		responsesType := renderer.Model.TypeWithTypeName(method.ResponsesTypeName)

		f.WriteLine(commentForText(method.Description))
		f.WriteLine(`func (client *Client) ` + method.ClientName + `(`)
		f.WriteLine(ParameterList(parametersType) + `) (`)
		if method.ResponsesTypeName == "" {
			f.WriteLine(`err error,`)
		} else {
			f.WriteLine(`response *` + method.ResponsesTypeName + `,`)
			f.WriteLine(`err error,`)
		}
		f.WriteLine(` ) {`)

		path := method.Path
		path = strings.Replace(path, "{+", "{", -1)
		f.WriteLine(`path := client.service + "` + path + `"`)

		if parametersType != nil {
			if parametersType.HasFieldWithPosition(surface.Position_PATH) {
				for _, field := range parametersType.Fields {
					if field.Position == surface.Position_PATH {
						f.WriteLine(`path = strings.Replace(path, "{` + field.Name + `}", fmt.Sprintf("%v", ` +
							field.ParameterName + `), 1)`)
					}
				}
			}
			if parametersType.HasFieldWithPosition(surface.Position_QUERY) {
				f.WriteLine(`v := url.Values{}`)
				for _, field := range parametersType.Fields {
					if field.Position == surface.Position_QUERY {
						if field.NativeType == "string" {
							f.WriteLine(`if (` + field.ParameterName + ` != "") {`)
							f.WriteLine(`  v.Set("` + field.Name + `", ` + field.ParameterName + `)`)
							f.WriteLine(`}`)
						}
					}
				}
				f.WriteLine(`if client.APIKey != "" {`)
				f.WriteLine(`  v.Set("key", client.APIKey)`)
				f.WriteLine(`}`)
				f.WriteLine(`if len(v) > 0 {`)
				f.WriteLine(`  path = path + "?" + v.Encode()`)
				f.WriteLine(`}`)
			}
		}

		if method.Method == "POST" {
			f.WriteLine(`body := new(bytes.Buffer)`)
			if parametersType != nil {
				f.WriteLine(`json.NewEncoder(body).Encode(` + parametersType.FieldWithPosition(surface.Position_BODY).Name + `)`)
			}
			f.WriteLine(`req, err := http.NewRequest("` + method.Method + `", path, body)`)
			f.WriteLine(`reqHeaders := make(http.Header)`)
			f.WriteLine(`reqHeaders.Set("Content-Type", "application/json")`)
			f.WriteLine(`req.Header = reqHeaders`)
		} else {
			f.WriteLine(`req, err := http.NewRequest("` + method.Method + `", path, nil)`)
		}
		f.WriteLine(`if err != nil {return}`)
		f.WriteLine(`resp, err := client.client.Do(req)`)
		f.WriteLine(`if err != nil {return}`)
		f.WriteLine(`defer resp.Body.Close()`)
		f.WriteLine(`if resp.StatusCode != 200 {`)

		if responsesType != nil {
			f.WriteLine(`	return nil, errors.New(resp.Status)`)
		} else {
			f.WriteLine(`	return errors.New(resp.Status)`)
		}
		f.WriteLine(`}`)

		if responsesType != nil {
			f.WriteLine(`response = &` + responsesType.Name + `{}`)

			f.WriteLine(`switch {`)
			// first handle everything that isn't "default"
			for _, responseField := range responsesType.Fields {
				if responseField.Name != "default" {
					f.WriteLine(`case resp.StatusCode == ` + responseField.Name + `:`)
					f.WriteLine(`  body, err := ioutil.ReadAll(resp.Body)`)
					f.WriteLine(`  if err != nil {return nil, err}`)
					f.WriteLine(`  result := &` + responseField.NativeType + `{}`)
					f.WriteLine(`  err = json.Unmarshal(body, result)`)
					f.WriteLine(`  if err != nil {return nil, err}`)
					f.WriteLine(`  response.` + responseField.FieldName + ` = result`)
				}
			}

			// then handle "default"
			hasDefault := false
			for _, responseField := range responsesType.Fields {
				if responseField.Name == "default" {
					hasDefault = true
					f.WriteLine(`default:`)
					f.WriteLine(`  defer resp.Body.Close()`)
					f.WriteLine(`  body, err := ioutil.ReadAll(resp.Body)`)
					f.WriteLine(`  if err != nil {return nil, err}`)
					f.WriteLine(`  result := &` + responseField.NativeType + `{}`)
					f.WriteLine(`  err = json.Unmarshal(body, result)`)
					f.WriteLine(`  if err != nil {return nil, err}`)
					f.WriteLine(`  response.` + responseField.FieldName + ` = result`)
				}
			}
			if !hasDefault {
				f.WriteLine(`default:`)
				f.WriteLine(`  break`)
			}
			f.WriteLine(`}`) // close switch statement
		}
		f.WriteLine("return")
		f.WriteLine("}")
	}

	return f.Bytes(), nil
}
