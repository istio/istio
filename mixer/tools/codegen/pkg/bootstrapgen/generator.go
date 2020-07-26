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

package bootstrapgen

import (
	"bytes"
	"fmt"
	"go/format"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"text/template"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	"golang.org/x/tools/imports"

	istio_policy_v1beta1 "istio.io/api/policy/v1beta1"
	descriptor2 "istio.io/istio/mixer/pkg/protobuf/descriptor"
	tmplPkg "istio.io/istio/mixer/tools/codegen/pkg/bootstrapgen/template"
	"istio.io/istio/mixer/tools/codegen/pkg/modelgen"
)

// Generator creates a Go file that will be build inside mixer framework. The generated file contains all the
// template specific code that mixer needs to add support for different passed in templates.
type Generator struct {
	OutFilePath   string
	ImportMapping map[string]string
}

const (
	fullGoNameOfValueTypePkgName = "istio_policy_v1beta1."
	strInt64                     = "int64"
	strString                    = "string"
	strBool                      = "bool"
	strFloat64                   = "float64"
)

// TODO share the code between this generator and the interfacegen code generator.
var primitiveToValueType = map[string]string{
	strString:              fullGoNameOfValueTypePkgName + istio_policy_v1beta1.STRING.String(),
	strBool:                fullGoNameOfValueTypePkgName + istio_policy_v1beta1.BOOL.String(),
	strInt64:               fullGoNameOfValueTypePkgName + istio_policy_v1beta1.INT64.String(),
	strFloat64:             fullGoNameOfValueTypePkgName + istio_policy_v1beta1.DOUBLE.String(),
	"map[string]string":    fullGoNameOfValueTypePkgName + istio_policy_v1beta1.STRING_MAP.String(),
	"net.IP":               fullGoNameOfValueTypePkgName + istio_policy_v1beta1.IP_ADDRESS.String(),
	"adapter.URI":          fullGoNameOfValueTypePkgName + istio_policy_v1beta1.URI.String(),
	"adapter.DNSName":      fullGoNameOfValueTypePkgName + istio_policy_v1beta1.DNS_NAME.String(),
	"adapter.EmailAddress": fullGoNameOfValueTypePkgName + istio_policy_v1beta1.EMAIL_ADDRESS.String(),

	"time.Duration": fullGoNameOfValueTypePkgName + istio_policy_v1beta1.DURATION.String(),
	"time.Time":     fullGoNameOfValueTypePkgName + istio_policy_v1beta1.TIMESTAMP.String(),
}

var aliasTypes = map[string]string{
	"adapter.DNSName":      strString,
	"adapter.EmailAddress": strString,
	"adapter.URI":          strString,
	"net.IP":               "[]byte",
	"int":                  strInt64,
	"map[string]string":    "attribute.WrapStringMap",
}

func containsValueTypeOrResMsg(ti modelgen.TypeInfo) bool {
	return ti.IsValueType || ti.IsResourceMessage || ti.IsMap && (ti.MapValue.IsValueType || ti.MapValue.IsResourceMessage)
}

type bootstrapModel struct {
	PkgName        string
	TemplateModels []*modelgen.Model
}

const goImportFmt = "\"%s\""
const resourceMsgTypeSuffix = "Type"
const resourceMsgInstanceSuffix = "Instance"
const resourceMsgInstParamSuffix = "InstanceParam"
const templateName = "Template"

// Generate creates a Go file that will be build inside mixer framework. The generated file contains all the
// template specific code that mixer needs to add support for different passed in templates.
func (g *Generator) Generate(fdsFiles map[string]string) error {
	return g.generateInternal(fdsFiles, tmplPkg.BootstrapTemplate, modelgen.Create)
}

func (g *Generator) generateInternal(fdsFiles map[string]string,
	tmplContent string,
	createModel func(parser *descriptor2.FileDescriptorSetParser) (*modelgen.Model, error)) error {
	imprts := make([]string, 0)
	tmpl, err := template.New("MixerBootstrap").Funcs(
		template.FuncMap{
			"getValueType": func(goType modelgen.TypeInfo) string {
				return primitiveToValueType[strings.Replace(goType.Name, " ", "", -1)]
			},
			"getUnspecifiedValueType": func() string {
				return fullGoNameOfValueTypePkgName + istio_policy_v1beta1.VALUE_TYPE_UNSPECIFIED.String()
			},
			"isAliasType": func(goType string) bool {
				_, found := aliasTypes[goType]
				return found
			},
			"getAliasType": func(goType string) string {
				return aliasTypes[goType]
			},
			"containsValueTypeOrResMsg": containsValueTypeOrResMsg,
			"reportTypeUsed": func(ti modelgen.TypeInfo) string {
				if len(ti.Import) > 0 {
					imprt := fmt.Sprintf(goImportFmt, ti.Import)
					if !contains(imprts, imprt) {
						imprts = append(imprts, imprt)
					}
				}
				// do nothing, just record the import so that we can add them later (only for the types that got printed)
				return ""
			},
			"getResourcMessageTypeName": func(s string) string {
				if s == templateName {
					return resourceMsgTypeSuffix
				}
				return s + resourceMsgTypeSuffix
			},
			"getResourcMessageInstanceName": func(s string) string {
				if s == templateName {
					return resourceMsgInstanceSuffix
				}
				return s
			},

			"getResourcMessageInterfaceParamTypeName": func(s string) string {
				if s == templateName {
					return resourceMsgInstParamSuffix
				}
				return s + resourceMsgInstParamSuffix
			},
			"getAllMsgs": func(model modelgen.Model) []modelgen.MessageInfo {
				res := make([]modelgen.MessageInfo, 0)
				res = append(res, model.TemplateMessage)
				res = append(res, model.ResourceMessages...)
				return res
			},
			"getTypeName": func(goType modelgen.TypeInfo) string {
				// GoType for a Resource message has a pointer reference. Therefore for a raw type name, we should strip
				// the "*".
				return strings.Trim(goType.Name, "*")
			},
			"getBuildFnName": func(typeName string) string {
				return "Build" + typeName
			},
			"getMessageBuilderName": func(m modelgen.Model, target interface{}) string {
				// Generate and return the name that will be used as the name of the struct that will be used
				// to build an instance and/or sub-message of an instance.
				// The struct will be initialized based on an instance parameter, and can be used to
				// efficiently construct instances during runtime.
				switch t := target.(type) {
				case modelgen.MessageInfo:
					return "builder_" + m.GoPackageName + "_" + t.Name
				case modelgen.TypeInfo:
					return "builder_" + m.GoPackageName + "_" + strings.Trim(t.Name, "*")
				case *modelgen.TypeInfo:
					return "builder_" + m.GoPackageName + "_" + strings.Trim(t.Name, "*")
				default:
					panic("unknown type in getMessageBuilderName" + reflect.TypeOf(target).String())
				}
			},
			"getNewMessageBuilderFnName": func(m modelgen.Model, target interface{}) string {
				// Generate and return the name of the function that will return a new builder struct
				// for this particular message. The function initializes the builder struct based on the
				// instance parameters and returns.
				switch t := target.(type) {
				case modelgen.MessageInfo:
					return "newBuilder_" + m.GoPackageName + "_" + t.Name
				case modelgen.TypeInfo:
					return "newBuilder_" + m.GoPackageName + "_" + strings.Trim(t.Name, "*")
				case *modelgen.TypeInfo:
					return "newBuilder_" + m.GoPackageName + "_" + strings.Trim(t.Name, "*")
				default:
					panic("unknown type in getNewMessageBuilderFnName:" + reflect.TypeOf(target).String())
				}
			},
			"builderFieldName": func(f modelgen.FieldInfo) string {
				// Returns the name of the field on the builder struct that will hold compiled.Expressions
				// or sub-builders for this particular field of the of the instance.
				return "bld" + f.GoName
			},
			"isPrimitiveType": func(goType modelgen.TypeInfo) bool {
				// Returns whether the given type is a primitive value, for evaluation purposes.
				// Primitives are: bool, int64, float64, and string.
				switch goType.Name {
				case strString, strBool, strInt64, strFloat64:
					return true
				default:
					return false
				}
			},
			"getEvalMethod": func(goType modelgen.TypeInfo) string {
				// Returns the name of the evaluation method that should be called on a compiled expression
				// for a given target type.
				switch goType.Name {
				case strString:
					return "EvaluateString"
				case strBool:
					return "EvaluateBoolean"
				case strInt64:
					return "EvaluateInteger"
				case strFloat64:
					return "EvaluateDouble"
				default:
					return "Evaluate"
				}
			},
			"getLocalVar": func(goType modelgen.TypeInfo) string {
				// Returns the name of the local variable to assign to when evaluating a compiled expression.
				switch goType.Name {
				case strString:
					return "vString"
				case strBool:
					return "vBool"
				case strInt64:
					return "vInt"
				case strFloat64:
					return "vDouble"
				default:
					return "vIface"
				}
			},
		}).Parse(tmplContent)

	if err != nil {
		return fmt.Errorf("cannot load template: %v", err)
	}

	models := make([]*modelgen.Model, 0)
	var fdss []string
	for k := range fdsFiles {
		fdss = append(fdss, k)
	}
	sort.Strings(fdss)

	for _, fdsPath := range fdss {
		var fds *descriptor.FileDescriptorSet
		fds, err = getFileDescSet(fdsPath)
		if err != nil {
			return fmt.Errorf("cannot parse file '%s' as a FileDescriptorSetProto. %v", fds, err)
		}

		parser := descriptor2.CreateFileDescriptorSetParser(fds, g.ImportMapping, fdsFiles[fdsPath])

		var model *modelgen.Model
		if model, err = createModel(parser); err != nil {
			return err
		}

		// TODO validate there is no ambiguity in template names.
		models = append(models, model)
	}

	pkgName := getParentDirName(g.OutFilePath)

	buf := new(bytes.Buffer)
	err = tmpl.Execute(buf, bootstrapModel{pkgName, models})
	if err != nil {
		return fmt.Errorf("cannot execute the template with the given data: %v", err)
	}
	bytesWithImpts := bytes.Replace(buf.Bytes(), []byte("$$additional_imports$$"), []byte(strings.Join(imprts, "\n")), 1)
	fmtd, err := format.Source(bytesWithImpts)
	if err != nil {
		return fmt.Errorf("could not format generated code: %v. Source code is %s", err, string(bytesWithImpts))
	}

	imports.LocalPrefix = "istio.io"
	// OutFilePath provides context for import path. We rely on the supplied bytes for content.
	imptd, err := imports.Process(g.OutFilePath, fmtd, &imports.Options{FormatOnly: true, Comments: true})
	if err != nil {
		return fmt.Errorf("could not fix imports for generated code: %v", err)
	}

	f, err := os.Create(g.OutFilePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() // nolint: gas
	if _, err = f.Write(imptd); err != nil {
		_ = os.Remove(f.Name()) // nolint: gas
		return err
	}
	return nil
}
func getParentDirName(filePath string) string {
	return filepath.Base(filepath.Dir(filePath))
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
