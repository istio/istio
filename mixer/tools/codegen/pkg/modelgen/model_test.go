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
		{"testdata/missing_template_name.descriptor_set", "Contains only one of the following two options"},
		{"testdata/missing_template_variety.descriptor_set", "Contains only one of the following two options"},
		{"testdata/missing_both_required.descriptor_set", "one proto file that has both extensions"},
		{"testdata/missing_template_message.descriptor_set", "message 'Template' not defined"},
		{"testdata/reserved_field_in_template.descriptor_set", "proto:15: Template message must not contain the reserved filed name 'Name'"},
		{"testdata/proto2_bad_syntax.descriptor_set", "Proto2BadSyntax.proto:3: Only proto3 template files are allowed."},
		{"testdata/unsupported_field_type_message.descriptor_set", "UnsupportedFieldTypeMessage.proto:13: " +
			"unsupported type for field 'o'. Supported types are 'string, int64, double, bool, " +
			"istio.mixer.v1.config.descriptor.ValueType, map<string, istio.mixer.v1.config.descriptor.ValueType>'"},
		{"testdata/unsupported_field_type_primitive.descriptor_set", "unsupported type for field 'o'."},
		{"testdata/unsupported_field_type_as_map.descriptor_set", "unsupported type for field 'o'."},
		{"testdata/unsupported_field_type_enum.descriptor_set", "unsupported type for field 'o'."},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.src), func(t *testing.T) {
			_, err := createTestModel(t, tt.src)

			if err == nil {
				t.Fatalf("CreateModel(%s) caused error 'nil', \n wanted err that contains string `%v`", tt.src, fmt.Errorf(tt.expectedError))
			} else if !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("CreateModel(%s) caused error '%v', \n wanted err that contains string `%v`", tt.src, err, fmt.Errorf(tt.expectedError))
			}
		})
	}
}

func TestBasicTopLevelFields(t *testing.T) {
	testFilename := "testdata/basic_top_level_fields.descriptor_set"
	model, _ := createTestModel(t,
		testFilename)
	if model.GoPackageName != "foo_bar" {
		t.Fatalf("CreateModel(%s).PackageName = %v, wanted %s", testFilename, model.GoPackageName, "foo_bar")
	}
	if model.Name != "List" {
		t.Fatalf("CreateModel(%s).Name = %v, wanted %s", testFilename, model.Name, "List")
	}
	if model.VarietyName != "TEMPLATE_VARIETY_CHECK" {
		t.Fatalf("CreateModel(%s).VarietyName = %v, wanted %s", testFilename, model.VarietyName, "TEMPLATE_VARIETY_CHECK")
	}
}

func TestInstanceFields(t *testing.T) {
	testFilename := "testdata/simple_template.descriptor_set"
	model, _ := createTestModel(t,
		testFilename)

	if len(model.InstanceStruct.Fields) != 3 {
		t.Fatalf("len(CreateModel(%s).InstanceStruct.Fields) = %v, wanted %d", testFilename, len(model.InstanceStruct.Fields), 3)
	}
	testField(t, testFilename, model.InstanceStruct.Fields, "Blacklist", "bool")
	testField(t, testFilename, model.InstanceStruct.Fields, "Val", "interface{}")
	testField(t, testFilename, model.InstanceStruct.Fields, "Dimensions", "map[string]interface{}")
}

func TestTypeFields(t *testing.T) {
	testFilename := "testdata/simple_template.descriptor_set"
	model, _ := createTestModel(t,
		testFilename)

	if len(model.TypeMessage.Fields) != 3 {
		t.Fatalf("len(CreateModel(%s).TypeMessage.Fields) = %v, wanted %d", testFilename, len(model.TypeMessage.Fields), 3)
	}
	testField(t, testFilename, model.TypeMessage.Fields, "blacklist", fullProtoNameOfValueTypeEnum)
	testField(t, testFilename, model.TypeMessage.Fields, "val", fullProtoNameOfValueTypeEnum)
	testField(t, testFilename, model.TypeMessage.Fields, "dimensions", fmt.Sprintf("map<string, %s>", fullProtoNameOfValueTypeEnum))
}

func TestConstructorParamFields(t *testing.T) {
	testFilename := "testdata/simple_template.descriptor_set"
	model, _ := createTestModel(t,
		testFilename)

	if len(model.ConstructorParamMessage.Fields) != 3 {
		t.Fatalf("len(CreateModel(%s).ConstructorParamMessage.Fields) = %v, wanted %d", testFilename, len(model.ConstructorParamMessage.Fields), 3)
	}
	testField(t, testFilename, model.ConstructorParamMessage.Fields, "blacklist", "bool")
	testField(t, testFilename, model.ConstructorParamMessage.Fields, "val", "string")
	testField(t, testFilename, model.ConstructorParamMessage.Fields, "dimensions", "map<string, string>")
}

func testField(t *testing.T, testFilename string, fields []fieldInfo, fldName string, expectedFldType string) {
	found := false
	for _, cf := range fields {
		if cf.Name == fldName {
			found = true
			if cf.Type.Name != expectedFldType {
				t.Fatalf("CreateModel(%s).ConstructorFields[%s] = %s, wanted %s", testFilename, fldName, cf.Type.Name, expectedFldType)
			}
		}
	}
	if !found {
		t.Fatalf("CreateModel(%s).ConstructorFields = %v, wanted to contain field with name '%s'", testFilename, fields, fldName)
	}
}

func createTestModel(t *testing.T, inputFDS string) (*Model, error) {
	fds, err := getFileDescSet(inputFDS)
	if err != nil {
		t.Fatalf("Unable to parse file descriptor set file %v", err)

	}

	parser, _ := CreateFileDescriptorSetParser(fds, map[string]string{})
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
