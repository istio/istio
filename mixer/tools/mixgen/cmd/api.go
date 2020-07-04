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
	"bytes"
	"fmt"
	"go/format"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	"github.com/spf13/cobra"
	"golang.org/x/tools/imports"

	"istio.io/istio/mixer/cmd/shared"
	descriptor2 "istio.io/istio/mixer/pkg/protobuf/descriptor"
	"istio.io/istio/mixer/tools/codegen/pkg/modelgen"
)

func apiGenCmd(fatalf shared.FormatFn) *cobra.Command {
	var templateFile string
	var outInterfaceFile string
	var oAugmentedTmplFile string
	var mappings []string
	adapterCmd := &cobra.Command{
		Use:   "api",
		Short: "creates type and service definitions from a given template",
		Run: func(cmd *cobra.Command, args []string) {
			var err error

			outInterfaceFile, err = filepath.Abs(outInterfaceFile)
			if err != nil {
				fatalf("Invalid path %s: %v", outInterfaceFile, err)
			}
			oAugmentedTmplFile, err = filepath.Abs(oAugmentedTmplFile)
			if err != nil {
				fatalf("Invalid path %s: %v", oAugmentedTmplFile, err)
			}
			importMapping := make(map[string]string)
			for _, maps := range mappings {
				m := strings.Split(maps, ":")
				if len(m) != 2 {
					fatalf("Invalid flag -m %v", mappings)
				}
				importMapping[strings.TrimSpace(m[0])] = strings.TrimSpace(m[1])
			}

			generator := Generator{OutInterfacePath: outInterfaceFile, OAugmentedTmplPath: oAugmentedTmplFile, ImptMap: importMapping}
			if err := generator.Generate(templateFile); err != nil {
				fatalf("%v", err)
			}
		},
	}
	adapterCmd.PersistentFlags().StringVarP(&templateFile, "template", "t", "", "Input "+
		"template file path")

	adapterCmd.PersistentFlags().StringVar(&outInterfaceFile, "go_out", "./generated.go", "Output "+
		"file path for generated template based go types and interfaces.")

	adapterCmd.PersistentFlags().StringVar(&oAugmentedTmplFile, "proto_out", "./generated_template.proto", "Output "+
		"file path for generated template based proto messages and services.")

	adapterCmd.PersistentFlags().StringArrayVarP(&mappings, "importmapping",
		"m", []string{},
		"colon separated mapping of proto import to Go package names."+
			" -m google/protobuf/descriptor.proto:github.com/golang/protobuf/protoc-gen-go/descriptor")
	return adapterCmd
}

const (
	resourceMsgSuffix            = "Msg"
	resourceMsgTypeSuffix        = "Type"
	resourceMsgInstParamSuffix   = "InstanceParam"
	fullGoNameOfValueTypeEnum    = "istio_policy_v1beta1.ValueType"
	fullProtoNameOfValueTypeEnum = "istio.policy.v1beta1.ValueType"
	goFileImportFmt              = `"%s"`
	protoFileImportFmt           = `import "%s";`
	protoValueTypeImport         = "policy/v1beta1/value_type.proto"
)

// Generator generates Go interfaces for adapters to implement for a given Template.
type Generator struct {
	OutInterfacePath   string
	OAugmentedTmplPath string
	ImptMap            map[string]string
}

func toProtoMap(k string, v string) string {
	return fmt.Sprintf("map<%s, %s>", k, v)
}

func containsValueType(ti modelgen.TypeInfo) bool {
	return ti.IsValueType || ti.IsMap && ti.MapValue.IsValueType
}

func valueTypeOrResMsg(ti modelgen.TypeInfo) bool {
	return ti.IsValueType || ti.IsResourceMessage || ti.IsMap && (ti.MapValue.IsValueType || ti.MapValue.IsResourceMessage)
}

// Generate creates a Go interfaces for adapters to implement for a given Template.
func (g *Generator) Generate(fdsFile string) error {
	return g.generateInternal(fdsFile, interfaceTemplate, augmentedProtoTmpl)
}

// Generate creates a Go interfaces for adapters to implement for a given Template.
func (g *Generator) generateInternal(fdsFile string, interfaceTmpl, augmentedProtoTmpl string) error {

	fds, err := getFileDescSet(fdsFile)
	if err != nil {
		return fmt.Errorf("cannot parse file '%s' as a FileDescriptorSetProto: %v", fdsFile, err)
	}

	parser := descriptor2.CreateFileDescriptorSetParser(fds, g.ImptMap, "")

	model, err := modelgen.Create(parser)
	if err != nil {
		return err
	}

	interfaceFileData, err := g.getInterfaceGoContent(model, interfaceTmpl)
	if err != nil {
		return err
	}

	augProtoData, err := g.getAugmentedProtoContent(model, model.PackageName, augmentedProtoTmpl)
	if err != nil {
		return err
	}

	// Everything succeeded, now write to the file.
	f1, err := os.Create(g.OutInterfacePath)
	if err != nil {
		return err
	}
	defer func() { _ = f1.Close() }() // nolint: gas

	if _, err = f1.Write(interfaceFileData); err != nil { // nolint: gas
		_ = f1.Close()           // nolint: gas
		_ = os.Remove(f1.Name()) // nolint: gas
		return err
	}

	f2, err := os.Create(g.OAugmentedTmplPath)
	if err != nil {
		return err
	}
	defer func() { _ = f2.Close() }() // nolint: gas
	if _, err = f2.Write(augProtoData); err != nil {
		_ = f2.Close()           // nolint: gas
		_ = os.Remove(f2.Name()) // nolint: gas
		return err
	}

	return nil
}

func (g *Generator) getInterfaceGoContent(model *modelgen.Model, interfaceTmpl string) ([]byte, error) {
	imprts := make([]string, 0)
	intfaceTmpl, err := template.New("ProcInterface").Funcs(
		template.FuncMap{
			"replaceGoValueTypeToInterface": func(typeInfo modelgen.TypeInfo) string {
				return strings.Replace(typeInfo.Name, fullGoNameOfValueTypeEnum, "interface{}", 1)
			},
			// The text/templates have code logic using which it decides the fields to be printed. Example
			// when printing 'Type' we skip fields that have static types. So, this callback method 'reportTypeUsed'
			// allows the template code to register which fields and types it actually printed. Based on what was actually
			// printed we can decide which imports should be added to the file. Therefore, import adding is a last step
			// after all fields and messages / structs are printed.
			// The template has a placeholder '$$imports$$' for printing the imports and
			// the this generator code will replace it with imports for fields that were recorded via this callback.
			"reportTypeUsed": func(ti modelgen.TypeInfo) string {
				if len(ti.Import) > 0 {
					imprt := fmt.Sprintf(goFileImportFmt, ti.Import)
					if !contains(imprts, imprt) {
						imprts = append(imprts, imprt)
					}
				}
				// do nothing, just record the import so that we can add them later (only for the types that got printed)
				return ""
			},
		}).Parse(interfaceTmpl)
	if err != nil {
		return nil, fmt.Errorf("cannot load template: %v", err)
	}
	intfaceBuf := new(bytes.Buffer)
	err = intfaceTmpl.Execute(intfaceBuf, model)
	if err != nil {
		return nil, fmt.Errorf("cannot execute the template with the given data: %v", err)
	}

	bytesWithImpts := bytes.Replace(intfaceBuf.Bytes(), []byte("$$additional_imports$$"), []byte(strings.Join(imprts, "\n")), 1)
	fmtd, err := format.Source(bytesWithImpts)
	if err != nil {
		return nil, fmt.Errorf("could not format generated code: %v : %s", err, intfaceBuf.String())
	}

	imports.LocalPrefix = "istio.io"
	// OutFilePath provides context for import path. We rely on the supplied bytes for content.
	imptd, err := imports.Process(g.OutInterfacePath, fmtd, nil)
	if err != nil {
		return nil, fmt.Errorf("could not fix imports for generated code: %v", err)
	}

	return imptd, nil
}

type stringifyFn func(modelgen.TypeInfo) string

func (g *Generator) getAugmentedProtoContent(model *modelgen.Model, pkgName string, augmentedProtoTmpl string) ([]byte, error) {
	imports := make([]string, 0)
	re := regexp.MustCompile(`(?i)` + pkgName + "\\.")

	var stringify stringifyFn

	trimPackageName := func(fullName string) string {
		return re.ReplaceAllString(fullName, "")
	}

	stringify = func(protoType modelgen.TypeInfo) string {
		if protoType.IsMap {
			return toProtoMap(stringify(*protoType.MapKey), stringify(*protoType.MapValue))
		}
		if protoType.IsResourceMessage {
			return trimPackageName(protoType.Name) + resourceMsgInstParamSuffix
		}
		return "string"
	}

	augmentedTemplateTmpl, err := template.New("AugmentedTemplateTmpl").Funcs(
		template.FuncMap{
			"valueTypeOrResMsg": valueTypeOrResMsg,
			"valueTypeOrResMsgFieldTypeName": func(protoTypeInfo modelgen.TypeInfo) string {
				if protoTypeInfo.IsResourceMessage {
					return trimPackageName(protoTypeInfo.Name) + resourceMsgTypeSuffix
				}
				if protoTypeInfo.IsMap && protoTypeInfo.MapValue.IsResourceMessage {
					return toProtoMap(protoTypeInfo.MapKey.Name, trimPackageName(protoTypeInfo.MapValue.Name)+resourceMsgTypeSuffix)
				}

				// convert Value msg to ValueType enum for use inside inferred type msg.
				if protoTypeInfo.IsValueType {
					return fullProtoNameOfValueTypeEnum
				}
				if protoTypeInfo.IsMap && protoTypeInfo.MapValue.IsValueType {
					return toProtoMap(protoTypeInfo.MapKey.Name, fullProtoNameOfValueTypeEnum)
				}

				return protoTypeInfo.Name
			},
			"stringify": stringify,
			"reportTypeUsed": func(ti modelgen.TypeInfo) string {
				if containsValueType(ti) {
					imptStm := fmt.Sprintf(protoFileImportFmt, protoValueTypeImport)
					if !contains(imports, imptStm) {
						imports = append(imports, imptStm)
					}
				}
				if len(ti.Import) > 0 {
					imprt := fmt.Sprintf(protoFileImportFmt, ti.Import)
					if !contains(imports, imprt) {
						imports = append(imports, imprt)
					}
				}
				// do nothing, just record the import so that we can add them later (only for the types that got printed)
				return ""
			},
			"getResourcMessageTypeName": func(s string) string {
				return s + resourceMsgTypeSuffix
			},
			"getResourcMessageInterfaceParamTypeName": func(s string) string {
				return s + resourceMsgInstParamSuffix
			},
			"typeName": func(protoTypeInfo modelgen.TypeInfo) string {
				if protoTypeInfo.IsResourceMessage {
					return trimPackageName(protoTypeInfo.Name) + resourceMsgSuffix
				}
				if protoTypeInfo.IsMap && protoTypeInfo.MapValue.IsResourceMessage {
					return toProtoMap(protoTypeInfo.MapKey.Name, trimPackageName(protoTypeInfo.MapValue.Name)+resourceMsgSuffix)
				}
				return protoTypeInfo.Name
			},
		},
	).Parse(augmentedProtoTmpl)
	if err != nil {
		return nil, fmt.Errorf("cannot load template: %v", err)
	}

	tmplBuf := new(bytes.Buffer)
	err = augmentedTemplateTmpl.Execute(tmplBuf, model)
	if err != nil {
		return nil, fmt.Errorf("cannot execute the template with the given data: %v", err)
	}

	return bytes.Replace(tmplBuf.Bytes(), []byte("$$additional_imports$$"), []byte(strings.Join(imports, "\n")), 1), nil
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

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
