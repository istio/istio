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
	surface "github.com/googleapis/gnostic/surface"
)

func (renderer *Renderer) RenderTypes() ([]byte, error) {
	f := NewLineWriter()
	f.WriteLine(`// GENERATED FILE: DO NOT EDIT!`)
	f.WriteLine(``)
	f.WriteLine(`package ` + renderer.Package)
	f.WriteLine(`// Types used by the API.`)
	for _, modelType := range renderer.Model.Types {
		f.WriteLine(`// ` + modelType.Description)
		if modelType.Kind == surface.TypeKind_STRUCT {
			f.WriteLine(`type ` + modelType.TypeName + ` struct {`)
			for _, field := range modelType.Fields {
				prefix := ""
				if field.Kind == surface.FieldKind_REFERENCE {
					prefix = "*"
				} else if field.Kind == surface.FieldKind_ARRAY {
					prefix = "[]"
				} else if field.Kind == surface.FieldKind_MAP {
					prefix = "map[string]"
				}
				f.WriteLine(field.FieldName + ` ` + prefix + field.NativeType + jsonTag(field))
			}
			f.WriteLine(`}`)
		} else if modelType.Kind == surface.TypeKind_OBJECT {
			f.WriteLine(`type ` + modelType.TypeName + ` map[string]` + modelType.ContentType)
		} else {
			f.WriteLine(`type ` + modelType.TypeName + ` interface {}`)
		}
	}
	return f.Bytes(), nil
}

func jsonTag(field *surface.Field) string {
	if field.Serialize {
		return " `json:" + `"` + field.Name + `,omitempty"` + "`"
	}
	return ""
}
