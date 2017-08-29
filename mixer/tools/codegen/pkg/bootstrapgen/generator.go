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

package bootstrapgen

import (
	"bytes"
	"fmt"
	"go/format"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	"golang.org/x/tools/imports"

	"istio.io/api/mixer/v1/config/descriptor"
	tmplPkg "istio.io/mixer/tools/codegen/pkg/bootstrapgen/template"
	"istio.io/mixer/tools/codegen/pkg/modelgen"
)

// Generator creates a Go file that will be build inside mixer framework. The generated file contains all the
// template specific code that mixer needs to add support for different passed in templates.
type Generator struct {
	OutFilePath   string
	ImportMapping map[string]string
}

const (
	fullGoNameOfValueTypePkgName = "istio_mixer_v1_config_descriptor."
)

// TODO share the code between this generator and the interfacegen code generator.
var primitiveToValueType = map[string]string{
	"string":        fullGoNameOfValueTypePkgName + istio_mixer_v1_config_descriptor.STRING.String(),
	"bool":          fullGoNameOfValueTypePkgName + istio_mixer_v1_config_descriptor.BOOL.String(),
	"int64":         fullGoNameOfValueTypePkgName + istio_mixer_v1_config_descriptor.INT64.String(),
	"float64":       fullGoNameOfValueTypePkgName + istio_mixer_v1_config_descriptor.DOUBLE.String(),
	"time.Duration": fullGoNameOfValueTypePkgName + istio_mixer_v1_config_descriptor.DURATION.String(),
	"time.Time":     fullGoNameOfValueTypePkgName + istio_mixer_v1_config_descriptor.TIMESTAMP.String(),
}

func containsValueType(ti modelgen.TypeInfo) bool {
	return ti.IsValueType || ti.IsMap && ti.MapValue.IsValueType
}

type bootstrapModel struct {
	PkgName        string
	TemplateModels []*modelgen.Model
}

const goImportFmt = "\"%s\""

// Generate creates a Go file that will be build inside mixer framework. The generated file contains all the
// template specific code that mixer needs to add support for different passed in templates.
func (g *Generator) Generate(fdsFiles map[string]string) error {
	imprts := make([]string, 0)
	tmpl, err := template.New("MixerBootstrap").Funcs(
		template.FuncMap{
			"getValueType": func(goType modelgen.TypeInfo) string {
				return primitiveToValueType[goType.Name]
			},
			"containsValueType": containsValueType,
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
		}).Parse(tmplPkg.InterfaceTemplate)

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

		var parser *modelgen.FileDescriptorSetParser
		parser, err = modelgen.CreateFileDescriptorSetParser(fds, g.ImportMapping, fdsFiles[fdsPath])
		if err != nil {
			return fmt.Errorf("cannot parse file '%s' as a FileDescriptorSetProto. %v", fds, err)
		}

		var model *modelgen.Model
		if model, err = modelgen.Create(parser); err != nil {
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
		return fmt.Errorf("could not format generated code: %v. Source code is %s", err, string(buf.Bytes()))
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
		_ = f.Close()           // nolint: gas
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
