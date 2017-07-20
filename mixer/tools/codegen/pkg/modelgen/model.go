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

package modelgen

import (
	"fmt"
	"strings"

	proto "github.com/gogo/protobuf/proto"

	tmpl "istio.io/mixer/pkg/adapter/template"
)

const fullNameOfValueTypeMessage = "istio_mixer_v1_config_descriptor.ValueType"

type (
	// Model represents the object used to code generate mixer artifacts.
	Model struct {
		// top level fields
		Name        string
		PackageName string
		VarietyName string

		// Constructor fields
		InstanceFields []fieldInfo

		// Warnings/Errors in the Template proto file.
		diags []diag
	}

	fieldInfo struct {
		Name string
		Type typeInfo
	}

	typeInfo struct {
		Name string
	}
)

// Create creates a Model object.
func Create(parser *FileDescriptorSetParser) (*Model, error) {

	templateProto, diags := getTmplFileDesc(parser.allFiles)
	if len(diags) != 0 {
		return nil, createError(diags)
	}

	// set the current generated code package to the package of the
	// templateProto. This will make sure references within the
	// generated file into the template's pb.go file are fully qualified.
	parser.packageName = goPackageName(templateProto.GetPackage())

	model := &Model{diags: make([]diag, 0)}

	model.fillModel(templateProto, parser)
	if len(model.diags) > 0 {
		return nil, createError(model.diags)
	}

	return model, nil
}

func createError(diags []diag) error {
	return fmt.Errorf("errors during parsing:\n%s", stringifyDiags(diags))
}

func (m *Model) fillModel(templateProto *FileDescriptor, parser *FileDescriptorSetParser) {

	m.addTopLevelFields(templateProto)

	// ensure Template is present
	if tmplDesc, ok := getRequiredMsg(templateProto, "Template"); !ok {
		m.addError(templateProto.GetName(), unknownLine, "message 'Template' not defined")
	} else {
		// TODO Validate that the type of the fields in the template are primitives that can be represented as ValueType
		// or is ValueType itself.
		m.addInstanceFields(parser, templateProto, tmplDesc)
	}
}

// Find the file that has the options TemplateVariety and TemplateName. There should only be one such file.
func getTmplFileDesc(fds []*FileDescriptor) (*FileDescriptor, []diag) {
	var templateDescriptorProto *FileDescriptor
	diags := make([]diag, 0)
	for _, fd := range fds {
		if fd.GetOptions() == nil {
			continue
		}
		if !proto.HasExtension(fd.GetOptions(), tmpl.E_TemplateName) && !proto.HasExtension(fd.GetOptions(), tmpl.E_TemplateVariety) {
			continue
		}

		if !proto.HasExtension(fd.GetOptions(), tmpl.E_TemplateName) || !proto.HasExtension(fd.GetOptions(), tmpl.E_TemplateVariety) {
			diags = append(diags, createDiag(errorDiag, fd.GetName(), unknownLine,
				"Contains only one of the following two options %s and %s. Both options are required.",
				[]interface{}{tmpl.E_TemplateVariety.Name, tmpl.E_TemplateName.Name}))
			continue
		}

		if templateDescriptorProto != nil {
			diags = append(diags, createDiag(errorDiag, fd.GetName(), unknownLine,
				"Proto files %s and %s, both have the options %s and %s. Only one proto file is allowed with those options",
				[]interface{}{fd.GetName(), templateDescriptorProto.Name, tmpl.E_TemplateVariety.Name, tmpl.E_TemplateName.Name}))
			continue
		}

		templateDescriptorProto = fd
	}

	if templateDescriptorProto == nil {
		diags = append(diags, createDiag(errorDiag, unknownFile, unknownLine, "There has to be one proto file that has both extensions %s and %s",
			[]interface{}{tmpl.E_TemplateVariety.Name, tmpl.E_TemplateVariety.Name}))
	}

	if len(diags) != 0 {
		return nil, diags
	}

	return templateDescriptorProto, nil
}

// Add all the file level fields to the model.
func (m *Model) addTopLevelFields(fd *FileDescriptor) {
	if !fd.proto3 {
		m.addError(fd.GetName(), fd.getLineNumber(syntaxPath), "Only proto3 template files are allowed")
	}

	if fd.Package != nil {
		m.PackageName = goPackageName(strings.TrimSpace(*fd.Package))
	} else {
		m.addError(fd.GetName(), unknownLine, "package name missing")
	}

	if tmplName, err := proto.GetExtension(fd.GetOptions(), tmpl.E_TemplateName); err == nil {
		if _, ok := tmplName.(*string); !ok {
			// protoc should mandate the type. It is impossible to get to this state.
			m.addError(fd.GetName(), unknownLine, "%s should be of type string", tmpl.E_TemplateName.Name)
		} else {
			m.Name = *tmplName.(*string)
		}
	} else {
		// This func should only get called for FileDescriptor that has this attribute,
		// therefore it is impossible to get to this state.
		m.addError(fd.GetName(), unknownLine, "file option %s is required", tmpl.E_TemplateName.Name)
	}

	if tmplVariety, err := proto.GetExtension(fd.GetOptions(), tmpl.E_TemplateVariety); err == nil {
		m.VarietyName = (*(tmplVariety.(*tmpl.TemplateVariety))).String()
	} else {
		// This func should only get called for FileDescriptor that has this attribute,
		// therefore it is impossible to get to this state.
		m.addError(fd.GetName(), unknownLine, "file option %s is required", tmpl.E_TemplateVariety.Name)
	}
}

// Build field information about the Instance struct.
func (m *Model) addInstanceFields(parser *FileDescriptorSetParser, templateProto *FileDescriptor, tmplDesc *Descriptor) {
	m.InstanceFields = make([]fieldInfo, 0)
	for i, fieldDesc := range tmplDesc.Field {
		fieldName := camelCase(fieldDesc.GetName())
		// Name field is a reserved field that will be injected in the Instance object. The user defined
		// Template should not have a Name field, else there will be a name clash.
		// 'Name' within the Instance object would represent the name of the Constructor:instance_name
		// specified in the operator Yaml file.
		if fieldName == "Name" {
			m.addError(tmplDesc.file.GetName(),
				templateProto.getLineNumber(getPathForField(tmplDesc, i)),
				"Template message must not contain the reserved filed name '%s'", fieldDesc.GetName())
			continue
		}
		typename := parser.GoType(tmplDesc.DescriptorProto, fieldDesc)
		typename = strings.Replace(typename, fullNameOfValueTypeMessage, "interface{}", 1)
		m.InstanceFields = append(m.InstanceFields, fieldInfo{Name: fieldName, Type: typeInfo{Name: typename}})
	}
}

func getRequiredMsg(fdp *FileDescriptor, msgName string) (*Descriptor, bool) {
	var cstrDesc *Descriptor
	for _, desc := range fdp.desc {
		if desc.GetName() == msgName {
			cstrDesc = desc
			break
		}
	}

	return cstrDesc, cstrDesc != nil
}
