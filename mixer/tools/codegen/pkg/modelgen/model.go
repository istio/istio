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

package modelgen

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"

	tmpl "istio.io/api/mixer/adapter/model/v1beta1"
	protoDesc "istio.io/istio/mixer/pkg/protobuf/descriptor"
)

const fullProtoNameOfValueMsg = "istio.policy.v1beta1.Value"
const customTypeImport = "policy/v1beta1/type.proto"

type typeMetadata struct {
	goName      string
	goImport    string
	protoImport string
}

// Hardcoded proto->go type mapping along with imports for the
// generated code.
var customMessageTypeMetadata = map[string]typeMetadata{
	".istio.policy.v1beta1.Duration": {
		goName:      "time.Duration",
		goImport:    "time",
		protoImport: customTypeImport,
	},
	".istio.policy.v1beta1.TimeStamp": {
		goName:      "time.Time",
		goImport:    "time",
		protoImport: customTypeImport,
	},
	".istio.policy.v1beta1.IPAddress": {
		goName:      "net.IP",
		protoImport: customTypeImport,
	},
	".istio.policy.v1beta1.DNSName": {
		goName:      "adapter.DNSName",
		protoImport: customTypeImport,
	},
	".istio.policy.v1beta1.EmailAddress": {
		goName:      "adapter.EmailAddress",
		protoImport: customTypeImport,
	},
	".istio.policy.v1beta1.Uri": {
		goName:      "adapter.URI",
		protoImport: customTypeImport,
	},
	".istio.policy.v1beta1.Value": {
		goName:      "interface{}",
		protoImport: customTypeImport,
	},
}

type (
	// Model represents the object used to code generate mixer artifacts.
	Model struct {
		TemplateName string
		VarietyName  string

		InterfaceName     string
		PackageImportPath string

		Comment string

		// Info for go interfaces
		GoPackageName string

		// Info for regenerated template proto
		PackageName     string
		TemplateMessage MessageInfo

		OutputTemplateMessage MessageInfo

		ResourceMessages []MessageInfo

		// Warnings/Errors in the Template proto file.
		diags []diag
	}

	// FieldInfo contains the data about the field
	FieldInfo struct {
		ProtoName string
		GoName    string
		Number    string // Only for proto fields
		Comment   string

		ProtoType TypeInfo
		GoType    TypeInfo
	}

	// TypeInfo contains the data about the field
	TypeInfo struct {
		Name              string
		IsRepeated        bool
		IsResourceMessage bool
		IsMap             bool
		IsValueType       bool
		MapKey            *TypeInfo
		MapValue          *TypeInfo
		Import            string
	}

	// MessageInfo contains the data about the type/message
	MessageInfo struct {
		Name    string
		Comment string
		Fields  []FieldInfo
	}
)

// Last segment of package name after the '.' must match the following regex
const tmplNameRegex = "^[a-zA-Z]+$"

var tmplNameRegexCompiled = regexp.MustCompile(tmplNameRegex)

// Create creates a Model object.
func Create(parser *protoDesc.FileDescriptorSetParser) (*Model, error) {

	templateProto, diags := getTmplFileDesc(parser.AllFiles)
	if len(diags) != 0 {
		return nil, createGoError(diags)
	}

	resourceFileDescs := getResourceDesc(parser.AllFiles, templateProto.GetPackage())

	// set the current generated code package to the package of the
	// templateProto. This will make sure references within the
	// generated file into the template's pb.go file are fully qualified.
	parser.PackageName = protoDesc.GoPackageName(templateProto.GetPackage())

	model := &Model{diags: make([]diag, 0), ResourceMessages: make([]MessageInfo, 0)}
	model.fillModel(templateProto, resourceFileDescs, parser)
	if len(model.diags) > 0 {
		return nil, createGoError(model.diags)
	}

	return model, nil
}

func createGoError(diags []diag) error {
	return fmt.Errorf("errors during parsing:\n%s", stringifyDiags(diags))
}

func (m *Model) fillModel(templateProto *protoDesc.FileDescriptor, resourceProtos []*protoDesc.FileDescriptor, parser *protoDesc.FileDescriptorSetParser) {
	m.PackageImportPath = parser.PackageImportPath

	m.addTopLevelFields(templateProto)
	valueTypeAllowedInFields := m.VarietyName != tmpl.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR.String()
	// ensure Template is present
	if tmplDesc, ok := getMsg(templateProto, "Template"); !ok {
		m.addError(templateProto.GetName(), unknownLine, "message 'Template' not defined")
	} else {
		diags := addMessageFields(parser,
			templateProto,
			tmplDesc,
			map[string]bool{"name": true},
			valueTypeAllowedInFields,
			&m.TemplateMessage,
		)
		m.TemplateMessage.Name = "Template"
		m.diags = append(m.diags, diags...)
	}

	// ensure OutputTemplate is present for APA and output-producing check adapters
	if m.VarietyName == tmpl.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR.String() || m.VarietyName == tmpl.TEMPLATE_VARIETY_CHECK_WITH_OUTPUT.String() {
		if outTmplDesc, ok := getMsg(templateProto, "OutputTemplate"); !ok {
			m.addError(templateProto.GetName(), unknownLine, "message 'OutputTemplate' not defined")
		} else {
			diags := addMessageFields(parser,
				templateProto,
				outTmplDesc,
				map[string]bool{"name": true},
				valueTypeAllowedInFields,
				&m.OutputTemplateMessage,
			)

			isPrimitiveValueType := func(typ TypeInfo) bool {
				if typ.IsValueType || typ.IsResourceMessage || typ.IsRepeated || (typ.IsMap && typ.MapValue.Name != "string") {
					return false
				}
				return true
			}

			// currently we only support output message to have flat list of fields that are of primitive types or
			// map<string, string> (Basically all the types supported by ValueType)
			// We can easily check this by checked if the type is not ValueType or ResourceMessage or a map<string, !string>
			for _, field := range m.OutputTemplateMessage.Fields {
				if !isPrimitiveValueType(field.GoType) {
					m.addError(templateProto.GetName(), unknownLine, "message 'OutputTemplate' field '%s' is of type '%s'."+
						" Only supported types in OutputTemplate message are : [string, int64, double, bool, "+
						"google.protobuf.Duration, google.protobuf.TimeStamp, map<string, string>]", field.ProtoName, field.ProtoType.Name)
				}
			}
			m.OutputTemplateMessage.Name = "OutputTemplate"
			m.diags = append(m.diags, diags...)
		}
	}

	for _, resourceProto := range resourceProtos {
		for _, desc := range resourceProto.Desc {
			if desc.GetName() != "Template" && desc.GetName() != "OutputTemplate" && !desc.GetOptions().GetMapEntry() {
				rescMsg := MessageInfo{Name: desc.GetName()}
				diags := addMessageFields(parser,
					resourceProto,
					desc,
					map[string]bool{"name": true},
					valueTypeAllowedInFields,
					&rescMsg,
				)
				m.ResourceMessages = append(m.ResourceMessages, rescMsg)
				m.diags = append(m.diags, diags...)
			}
		}
	}
}

func addMessageFields(parser *protoDesc.FileDescriptorSetParser, fileDesc *protoDesc.FileDescriptor, msgDesc *protoDesc.Descriptor,
	reservedNames map[string]bool, valueTypeAllowed bool, outMessage *MessageInfo) []diag {
	diags := make([]diag, 0)
	outMessage.Comment = fileDesc.GetComment(msgDesc.Path)
	outMessage.Fields = make([]FieldInfo, 0)
	for i, fieldDesc := range msgDesc.Field {
		fieldName := fieldDesc.GetName()

		if _, ok := reservedNames[strings.ToLower(fieldName)]; ok {
			err := createError(
				msgDesc.FileDesc.GetName(),
				fileDesc.GetLineNumber(protoDesc.GetPathForField(msgDesc, i)),
				"%s message must not contain the reserved field name '%s'",
				msgDesc.GetName(), fieldDesc.GetName())

			diags = append(diags, err)
			continue
		}

		protoTypeInfo, goTypeInfo, err := getTypeName(parser, fieldDesc, valueTypeAllowed)
		if err != nil {
			err := createError(msgDesc.FileDesc.GetName(), fileDesc.GetLineNumber(protoDesc.GetPathForField(msgDesc, i)), err.Error())
			diags = append(diags, err)
		}
		outMessage.Fields = append(outMessage.Fields, FieldInfo{
			ProtoName: fieldName,
			GoName:    protoDesc.CamelCase(fieldName),
			GoType:    goTypeInfo,
			ProtoType: protoTypeInfo,
			Number:    strconv.Itoa(int(fieldDesc.GetNumber())),
			Comment:   fileDesc.GetComment(protoDesc.GetPathForField(msgDesc, i)),
		})
	}

	return diags
}

// get all file descriptors that has the same package as the Template message.
func getResourceDesc(fds []*protoDesc.FileDescriptor, pkg string) []*protoDesc.FileDescriptor {
	result := make([]*protoDesc.FileDescriptor, 0)
	for _, fd := range fds {
		if fd.GetPackage() == pkg {
			result = append(result, fd)
		}
	}
	return result
}

// Find the file that has the options TemplateVariety and TemplateName. There should only be one such file.
func getTmplFileDesc(fds []*protoDesc.FileDescriptor) (*protoDesc.FileDescriptor, []diag) {
	var templateDescriptorProto *protoDesc.FileDescriptor
	diags := make([]diag, 0)
	for _, fd := range fds {
		if fd.GetOptions() == nil {
			continue
		}
		if !proto.HasExtension(fd.GetOptions(), tmpl.E_TemplateVariety) {
			continue
		}

		if templateDescriptorProto != nil {
			diags = append(diags, createError(fd.GetName(), unknownLine,
				"Proto files %s and %s, both have the option %s. Only one proto file is allowed with this options",
				fd.GetName(), templateDescriptorProto.GetName(), tmpl.E_TemplateVariety.Name))
			continue
		}

		templateDescriptorProto = fd
	}

	if templateDescriptorProto == nil {
		diags = append(diags, createError(unknownFile, unknownLine, "There has to be one proto file that has the extension %s",
			tmpl.E_TemplateVariety.Name))
	}

	if len(diags) != 0 {
		return nil, diags
	}

	return templateDescriptorProto, nil
}

// Add all the file level fields to the model.
func (m *Model) addTopLevelFields(fd *protoDesc.FileDescriptor) {
	if !fd.Proto3 {
		m.addError(fd.GetName(), fd.GetLineNumber(protoDesc.SyntaxPath), "Only proto3 template files are allowed")
	}

	if fd.Package != nil {
		m.PackageName = strings.ToLower(strings.TrimSpace(fd.GetPackage()))
		m.GoPackageName = protoDesc.GoPackageName(m.PackageName)
		if lastSeg, err := getLastSegment(strings.TrimSpace(fd.GetPackage())); err != nil {
			m.addError(fd.GetName(), fd.GetLineNumber(protoDesc.PackagePath), err.Error())
		} else {
			// capitalize the first character since this string is used to create function names.
			m.InterfaceName = strings.Title(lastSeg)
			m.TemplateName = strings.ToLower(lastSeg)
		}
	} else {
		m.addError(fd.GetName(), unknownLine, "package name missing")
	}

	// if explicit name is provided, use it.
	if tmplName, err := proto.GetExtension(fd.GetOptions(), tmpl.E_TemplateName); err == nil {
		tmplNameHint := *(tmplName.(*string))
		if !tmplNameRegexCompiled.MatchString(tmplNameHint) {
			m.addError(fd.GetName(), unknownLine, "the template_name option '%s' must match the regex '%s'",
				tmplNameHint, tmplNameRegex)
		} else {
			m.InterfaceName = strings.Title(tmplNameHint)
			m.TemplateName = strings.ToLower(tmplNameHint)
		}
	}

	if tmplVariety, err := proto.GetExtension(fd.GetOptions(), tmpl.E_TemplateVariety); err == nil {
		m.VarietyName = (tmplVariety.(*tmpl.TemplateVariety)).String()
	}

	// For file level comments, comments from multiple locations are composed.
	m.Comment = fmt.Sprintf("%s\n%s", fd.GetComment(protoDesc.SyntaxPath), fd.GetComment(protoDesc.PackagePath))
}

func getLastSegment(pkg string) (string, error) {
	segs := strings.Split(pkg, ".")
	last := segs[len(segs)-1]
	if tmplNameRegexCompiled.MatchString(last) {
		return last, nil
	}
	return "", fmt.Errorf("the last segment of package name '%s' must match the regex '%s'", pkg, tmplNameRegex)
}

func getMsg(fdp *protoDesc.FileDescriptor, msgName string) (*protoDesc.Descriptor, bool) {
	var cstrDesc *protoDesc.Descriptor
	for _, desc := range fdp.Desc {
		if desc.GetName() == msgName {
			cstrDesc = desc
			break
		}
	}

	return cstrDesc, cstrDesc != nil
}

func createInvalidTypeError(field string, valueTypeAllowed bool, extraErr error) error {
	var supTypes []string
	if valueTypeAllowed {
		supTypes = append([]string{fullProtoNameOfValueMsg}, simpleTypes...)
	} else {
		supTypes = simpleTypes
	}

	errStr := fmt.Sprintf("unsupported type for field '%s'. Supported types are '%s'", field, getAllSupportedTypes(supTypes...))
	if extraErr == nil {
		return fmt.Errorf(errStr)
	}
	return fmt.Errorf(errStr+"; %v", extraErr)
}

var simpleTypes = []string{
	"string",
	"int64",
	"double",
	"bool",
	"other messages defined within the same package",
}

func getAllSupportedTypes(simpleTypes ...string) string {
	return strings.Join(simpleTypes, ", ") + ", map<string, any of the listed supported types>"
}

func getTypeName(g *protoDesc.FileDescriptorSetParser, field *descriptor.FieldDescriptorProto,
	valueTypeAllowed bool) (protoType TypeInfo, goType TypeInfo, err error) {
	proto, golang, err := getTypeNameRec(g, field, valueTypeAllowed)
	// `repeated` is not part of the type descriptor, instead it is on the field. So we have to separately set it on
	// the TypeInfo object.
	if err == nil && !proto.IsMap && field.IsRepeated() {
		proto.IsRepeated = true
		proto.Name = getProtoArray(proto.Name)
		golang.IsRepeated = true
		golang.Name = getGoArray(golang.Name)
	}
	return proto, golang, err
}
func getGoArray(typeName string) string {
	return "[]" + typeName
}
func getProtoArray(typeName string) string {
	return "repeated " + typeName
}

const (
	sINT64   = "int64"
	sFLOAT64 = "float64"
	sBOOL    = "bool"
	sSTRING  = "string"
)

func getTypeNameRec(g *protoDesc.FileDescriptorSetParser, field *descriptor.FieldDescriptorProto, valueTypeAllowed bool) (
	protoType TypeInfo, goType TypeInfo, err error) {
	switch *field.Type {
	case descriptor.FieldDescriptorProto_TYPE_STRING:
		return TypeInfo{Name: "string"}, TypeInfo{Name: sSTRING}, nil
	case descriptor.FieldDescriptorProto_TYPE_INT64:
		return TypeInfo{Name: "int64"}, TypeInfo{Name: sINT64}, nil
	case descriptor.FieldDescriptorProto_TYPE_DOUBLE:
		return TypeInfo{Name: "double"}, TypeInfo{Name: sFLOAT64}, nil
	case descriptor.FieldDescriptorProto_TYPE_BOOL:
		return TypeInfo{Name: "bool"}, TypeInfo{Name: sBOOL}, nil
	case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
		if v, ok := customMessageTypeMetadata[field.GetTypeName()]; ok {
			pType := TypeInfo{Name: field.GetTypeName()[1:], Import: v.protoImport}
			gType := TypeInfo{Name: v.goName, Import: v.goImport}
			if field.GetTypeName()[1:] == fullProtoNameOfValueMsg {
				pType.IsValueType = true
				gType.IsValueType = true
				if !valueTypeAllowed {
					return TypeInfo{}, TypeInfo{}, createInvalidTypeError(field.GetName(), valueTypeAllowed, nil)
				}
			}
			return pType, gType, nil
		}
		desc := g.ObjectNamed(field.GetTypeName())
		if d, ok := desc.(*protoDesc.Descriptor); ok && d.GetOptions().GetMapEntry() {
			keyField, valField := d.Field[0], d.Field[1]

			protoKeyType, goKeyType, err := getTypeNameRec(g, keyField, valueTypeAllowed)
			if err != nil || protoKeyType.Name != "string" {
				return TypeInfo{}, TypeInfo{}, createInvalidTypeError(field.GetName(), valueTypeAllowed, err)
			}

			protoValType, goValType, err := getTypeNameRec(g, valField, valueTypeAllowed)
			if err != nil {
				return TypeInfo{}, TypeInfo{}, createInvalidTypeError(field.GetName(), valueTypeAllowed, err)
			}

			return TypeInfo{
					Name:     fmt.Sprintf("map<%s, %s>", protoKeyType.Name, protoValType.Name),
					IsMap:    true,
					MapKey:   &protoKeyType,
					MapValue: &protoValType,
					Import:   protoValType.Import,
				},
				TypeInfo{
					Name:     fmt.Sprintf("map[%s]%s", goKeyType.Name, goValType.Name),
					IsMap:    true,
					MapKey:   &goKeyType,
					MapValue: &goValType,
					Import:   goValType.Import,
				},
				nil
		}
		return TypeInfo{Name: field.GetTypeName()[1:], IsResourceMessage: true}, TypeInfo{Name: "*" + g.TypeName(desc), IsResourceMessage: true}, nil

	}
	return TypeInfo{}, TypeInfo{}, createInvalidTypeError(field.GetName(), valueTypeAllowed, nil)
}
