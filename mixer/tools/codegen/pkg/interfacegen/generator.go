// Copyright 2017 Istio Authors
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

package interfacegen

import (
	"bytes"
	"fmt"
	"go/format"
	"io/ioutil"
	"os"
	"strings"
	"text/template"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	"golang.org/x/tools/imports"

	tmpl "istio.io/mixer/tools/codegen/pkg/interfacegen/template"
	"istio.io/mixer/tools/codegen/pkg/modelgen"
)

// Generator generates Go interfaces for adapters to implement for a given Template.
type Generator struct {
	OutInterfacePath   string
	OAugmentedTmplPath string
	ImptMap            map[string]string
}

const (
	fullGoNameOfValueTypeEnum = "istio_mixer_v1_config_descriptor.ValueType"
)

func toProtoMap(k string, v string) string {
	return fmt.Sprintf("map<%s, %s>", k, v)
}

func stringify(protoType modelgen.TypeInfo) string {
	if protoType.IsMap {
		return toProtoMap(stringify(*protoType.MapKey), stringify(*protoType.MapValue))
	}
	return "string"
}

func containsValueType(ti modelgen.TypeInfo) bool {
	return ti.IsValueType || ti.IsMap && ti.MapValue.IsValueType
}

// Generate creates a Go interfaces for adapters to implement for a given Template.
func (g *Generator) Generate(fdsFile string) error {
	intfaceTmpl, err := template.New("ProcInterface").Funcs(
		template.FuncMap{
			"replaceGoValueTypeToInterface": func(typeInfo modelgen.TypeInfo) string {
				return strings.Replace(typeInfo.Name, fullGoNameOfValueTypeEnum, "interface{}", 1)
			},
		}).Parse(tmpl.InterfaceTemplate)
	if err != nil {
		return fmt.Errorf("cannot load template: %v", err)
	}

	fds, err := getFileDescSet(fdsFile)
	if err != nil {
		return fmt.Errorf("cannot parse file '%s' as a FileDescriptorSetProto: %v", fdsFile, err)
	}

	parser, err := modelgen.CreateFileDescriptorSetParser(fds, g.ImptMap, "")
	if err != nil {
		return fmt.Errorf("cannot parse file '%s' as a FileDescriptorSetProto: %v", fdsFile, err)
	}

	model, err := modelgen.Create(parser)
	if err != nil {
		return err
	}

	intfaceBuf := new(bytes.Buffer)
	err = intfaceTmpl.Execute(intfaceBuf, model)
	if err != nil {
		return fmt.Errorf("cannot execute the template with the given data: %v", err)
	}

	fmtd, err := format.Source(intfaceBuf.Bytes())
	if err != nil {
		return fmt.Errorf("could not format generated code: %v : %s", err, string(intfaceBuf.Bytes()))
	}

	imports.LocalPrefix = "istio.io"
	// OutFilePath provides context for import path. We rely on the supplied bytes for content.
	imptd, err := imports.Process(g.OutInterfacePath, fmtd, nil)
	if err != nil {
		return fmt.Errorf("could not fix imports for generated code: %v", err)
	}

	augmentedTemplateTmpl, err := template.New("AugmentedTemplateTmpl").Funcs(
		template.FuncMap{
			"containsValueType": containsValueType,
			"stringify":         stringify,
		}).Parse(tmpl.RevisedTemplateTmpl)
	if err != nil {
		return fmt.Errorf("cannot load template: %v", err)
	}

	tmplBuf := new(bytes.Buffer)
	err = augmentedTemplateTmpl.Execute(tmplBuf, model)
	if err != nil {
		return fmt.Errorf("cannot execute the template with the given data: %v", err)
	}

	// Everything succeeded, now write to the file.
	f1, err := os.Create(g.OutInterfacePath)
	if err != nil {
		return err
	}
	defer func() { _ = f1.Close() }()

	if _, err = f1.Write(imptd); err != nil {
		_ = f1.Close()
		_ = os.Remove(f1.Name())
		return err
	}

	f2, err := os.Create(g.OAugmentedTmplPath)
	if err != nil {
		return err
	}
	defer func() { _ = f2.Close() }()
	if _, err = f2.Write(tmplBuf.Bytes()); err != nil {
		_ = f2.Close()
		_ = os.Remove(f2.Name())
		return err
	}

	return nil
}

func getFileDescSet(path string) (*descriptor.FileDescriptorSet, error) {
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	fds := &descriptor.FileDescriptorSet{}
	err = proto.Unmarshal(bytes, fds)
	if err != nil {
		return nil, err
	}

	return fds, nil
}
