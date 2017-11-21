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
	"io/ioutil"
	"reflect"
	"strings"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
)

func TestErrorInTemplate(t *testing.T) {
	tests := []struct {
		src           string
		expectedError string
	}{
		{"testdata/missing_package_name.descriptor_set", "package name missing"},
		{"testdata/missing_both_required.descriptor_set", "There has to be one proto file that has the " +
			"extension istio.mixer.v1.template.template_variety"},
		{"testdata/missing_template_message.descriptor_set", "message 'Template' not defined"},
		{"testdata/reserved_field_in_template.descriptor_set", "proto:14: Template message must not contain the reserved field name 'Name'"},
		{"testdata/proto2_bad_syntax.descriptor_set", "Proto2BadSyntax.proto:3: Only proto3 template files are allowed."},
		{"testdata/unsupported_field_type_primitive.descriptor_set", "unsupported type for field 'o'. " +
			"Supported types are 'string, int64, double, bool, istio.mixer.v1.config.descriptor.ValueType, other messages" +
			" defined within the same package, map<string, any of the listed supported types>"},
		{"testdata/unsupported_field_type_as_map.descriptor_set", "unsupported type for field 'o'."},
		{"testdata/unsupported_field_type_enum.descriptor_set", "unsupported type for field 'o'."},
		{"testdata/wrong_pkg_name.descriptor_set", "WrongPkgName.proto:2: the last segment of package " +
			"name 'foo.badStrNumbersNotAllowed123' must match the reges '^[a-zA-Z]+$'"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.src), func(t *testing.T) {
			_, err := createTestModel(t, tt.src)

			if err == nil {
				t.Fatalf("CreateModel(%s) caused error 'nil', \n wanted err that contains string `%v`",
					tt.src, fmt.Errorf(tt.expectedError))
			} else if !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("CreateModel(%s) caused error\n%v;wanted err that contains string\n%v",
					tt.src, err, fmt.Errorf(tt.expectedError))
			}
		})
	}
}

func TestBasicTopLevelFields(t *testing.T) {
	testFilename := "testdata/basic_top_level_fields.descriptor_set"
	model, err := createTestModel(t,
		testFilename)
	if err != nil {
		t.Fatalf("model creation failed %v", err)
	}
	if model.GoPackageName != "foo_listchecker" {
		t.Errorf("CreateModel(%s).PackageName = %v, wanted %s", testFilename, model.GoPackageName, "foo_listchecker")
	}
	if model.InterfaceName != "ListChecker" {
		t.Errorf("CreateModel(%s).Name = %v, wanted %s", testFilename, model.InterfaceName, "ListChecker")
	}
	if model.TemplateName != "listchecker" {
		t.Errorf("CreateModel(%s).Name = %v, wanted %s", testFilename, model.InterfaceName, "listchecker")
	}
	if model.VarietyName != "TEMPLATE_VARIETY_CHECK" {
		t.Errorf("CreateModel(%s).VarietyName = %v, wanted %s", testFilename, model.VarietyName, "TEMPLATE_VARIETY_CHECK")
	}
	if model.TemplateMessage.Comment != "// My Template comment" {
		t.Errorf("CreateModel(%s).TemplateMessage.Comment = %s, wanted %s", testFilename, model.TemplateMessage.Comment, "// My Template comment")
	}

	if model.Comment != "// comment for syntax\n// comment for package" {
		t.Errorf("CreateModel(%s).Comment = %s, wanted %s", testFilename, model.Comment, "// comment for syntax\n// comment for package")
	}
}

func TestTypeFields(t *testing.T) {
	model, _ := createTestModel(t,
		"testdata/simple_template.descriptor_set")

	testCompleteFieldList(model.TemplateMessage, t)
	var res3MsgInfo MessageInfo
	for _, j := range model.ResourceMessages {
		if j.Name == "Resource3" {
			res3MsgInfo = j
		}
	}
	testCompleteFieldList(res3MsgInfo, t)
}

func testCompleteFieldList(msgInfo MessageInfo, t *testing.T) {
	if len(msgInfo.Fields) != 12 {
		t.Fatalf("len(CreateModel(%s).TypeMessage.Fields) = %v, wanted %d", "testdata/simple_template", len(msgInfo.Fields), 12)
	}
	testField(t, msgInfo.Fields,
		"blacklist", TypeInfo{Name: "bool"}, "Blacklist", TypeInfo{Name: "bool"}, "multi line comment line 2")
	testField(t, msgInfo.Fields,
		"fieldInt64", TypeInfo{Name: "int64"},
		"FieldInt64", TypeInfo{Name: "int64"}, "")
	testField(t, msgInfo.Fields,
		"fieldString", TypeInfo{Name: "string"},
		"FieldString", TypeInfo{Name: "string"}, "")
	testField(t, msgInfo.Fields,
		"fieldDouble", TypeInfo{Name: "double"},
		"FieldDouble", TypeInfo{Name: "float64"}, "")
	testField(t, msgInfo.Fields,
		"val",
		TypeInfo{Name: "istio.mixer.v1.config.descriptor.ValueType", IsValueType: true}, "Val",
		TypeInfo{Name: "istio_mixer_v1_config_descriptor.ValueType", IsValueType: true}, "single line block comment")
	testField(t, msgInfo.Fields,
		"dimensions",
		TypeInfo{Name: "map<string, istio.mixer.v1.config.descriptor.ValueType>",
			IsMap:    true,
			MapKey:   &TypeInfo{Name: "string"},
			MapValue: &TypeInfo{Name: "istio.mixer.v1.config.descriptor.ValueType", IsValueType: true},
		},
		"Dimensions",
		TypeInfo{
			Name:     "map[string]istio_mixer_v1_config_descriptor.ValueType",
			IsMap:    true,
			MapKey:   &TypeInfo{Name: "string"},
			MapValue: &TypeInfo{Name: "istio_mixer_v1_config_descriptor.ValueType", IsValueType: true},
		}, "single line comment")
	testField(t, msgInfo.Fields,
		"dimensionsConstInt64Val",
		TypeInfo{Name: "map<string, int64>",
			IsMap:    true,
			MapKey:   &TypeInfo{Name: "string"},
			MapValue: &TypeInfo{Name: "int64"},
		},
		"DimensionsConstInt64Val",
		TypeInfo{
			Name:     "map[string]int64",
			IsMap:    true,
			MapKey:   &TypeInfo{Name: "string"},
			MapValue: &TypeInfo{Name: "int64"},
		}, "")
	testField(t, msgInfo.Fields,
		"dimensionsConstStringVal",
		TypeInfo{Name: "map<string, string>",
			IsMap:    true,
			MapKey:   &TypeInfo{Name: "string"},
			MapValue: &TypeInfo{Name: "string"},
		},
		"DimensionsConstStringVal",
		TypeInfo{
			Name:     "map[string]string",
			IsMap:    true,
			MapKey:   &TypeInfo{Name: "string"},
			MapValue: &TypeInfo{Name: "string"},
		}, "")
	testField(t, msgInfo.Fields,
		"dimensionsConstBoolVal",
		TypeInfo{Name: "map<string, bool>",
			IsMap:    true,
			MapKey:   &TypeInfo{Name: "string"},
			MapValue: &TypeInfo{Name: "bool"},
		},
		"DimensionsConstBoolVal",
		TypeInfo{
			Name:     "map[string]bool",
			IsMap:    true,
			MapKey:   &TypeInfo{Name: "string"},
			MapValue: &TypeInfo{Name: "bool"},
		}, "")
	testField(t, msgInfo.Fields,
		"dimensionsConstDoubleVal",
		TypeInfo{Name: "map<string, double>",
			IsMap:    true,
			MapKey:   &TypeInfo{Name: "string"},
			MapValue: &TypeInfo{Name: "double"},
		},
		"DimensionsConstDoubleVal",
		TypeInfo{
			Name:     "map[string]float64",
			IsMap:    true,
			MapKey:   &TypeInfo{Name: "string"},
			MapValue: &TypeInfo{Name: "float64"},
		}, "")
	testField(t, msgInfo.Fields,
		"res3_list",
		TypeInfo{Name: "repeated foo.bar.Resource3",
			IsResourceMessage: true,
			IsRepeated:        true,
		},
		"Res3List",
		TypeInfo{
			Name:              "[]*Resource3",
			IsResourceMessage: true,
			IsRepeated:        true,
		}, "")
	testField(t, msgInfo.Fields,
		"res3_map",
		TypeInfo{Name: "map<string, foo.bar.Resource3>",
			IsResourceMessage: false,
			IsMap:             true,
			MapKey:            &TypeInfo{Name: "string"},
			MapValue:          &TypeInfo{Name: "foo.bar.Resource3", IsResourceMessage: true},
		},
		"Res3Map",
		TypeInfo{
			Name:              "map[string]*Resource3",
			IsResourceMessage: false,
			IsMap:             true,
			MapKey:            &TypeInfo{Name: "string"},
			MapValue:          &TypeInfo{Name: "*Resource3", IsResourceMessage: true},
		}, "")
}

func testField(t *testing.T, fields []FieldInfo, protoFldName string, protoFldType TypeInfo,
	goFldName string, goFldType TypeInfo, comment string) {
	testFilename := "testdata/simple_template.descriptor_set"
	found := false
	for _, cf := range fields {
		if cf.ProtoName == protoFldName {
			found = true
			if cf.GoName != goFldName ||
				!reflect.DeepEqual(cf.ProtoType, protoFldType) ||
				!reflect.DeepEqual(cf.GoType, goFldType) ||
				!strings.Contains(cf.Comment, comment) {
				t.Fatalf("Got CreateModel(%s).TemplateMessage.Fields[%s] = \nGoName:%s, ProtoType:%v, GoType:%v, Comment:%s"+
					";wanted\nGoName:%s, ProtoType:%v, GoType:%v, comment: %s",
					testFilename, protoFldName, cf.GoName, cf.ProtoType, cf.GoType, cf.Comment, goFldName, protoFldType, goFldType, comment)
			}
		}
	}
	if !found {
		t.Fatalf("CreateModel(%s).TemplateMessage = %v, wanted to contain field with name '%s'", testFilename, fields, protoFldName)
	}
}

func createTestModel(t *testing.T, inputFDS string) (*Model, error) {
	fds, err := getFileDescSet(inputFDS)
	if err != nil {
		t.Fatalf("Unable to parse file descriptor set file %v", err)

	}

	parser, _ := CreateFileDescriptorSetParser(fds, map[string]string{}, "")
	return Create(parser)
}

func getFileDescSet(path string) (*descriptor.FileDescriptorSet, error) {
	byts, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	fds := &descriptor.FileDescriptorSet{}
	err = proto.Unmarshal(byts, fds)

	return fds, err
}
