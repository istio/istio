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

// nolint
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/BasicTopLevelFields.proto -otestdata/BasicTopLevelFields.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/ExplicitTemplateName.proto -otestdata/ExplicitTemplateName.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/BadExplicitTemplateName.proto -otestdata/BadExplicitTemplateName.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh testdata/Proto2BadSyntax.proto -otestdata/noSourceInfoNoImportsBadProto.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info  testdata/multiFileOne.proto testdata/multiFileTwo.proto -otestdata/multiFile.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/MissingBothRequiredExt.proto -otestdata/MissingBothRequiredExt.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/MissingPackageName.proto -otestdata/MissingPackageName.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/MissingTemplateMessage.proto -otestdata/MissingTemplateMessage.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/MissingTemplateVarietyExt.proto -otestdata/MissingTemplateVarietyExt.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/Proto2BadSyntax.proto -otestdata/Proto2BadSyntax.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/ReservedFieldInTemplate.proto -otestdata/ReservedFieldInTemplate.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/SimpleApaTemplate.proto -otestdata/SimpleApaTemplate.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/SimpleTemplate.proto -otestdata/SimpleTemplate.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/UnsupportedMapVal.proto -otestdata/UnsupportedMapVal.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/UnsupportedMapKey.proto -otestdata/UnsupportedMapKey.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/UnsupportedFieldTypeEnum.proto -otestdata/UnsupportedFieldTypeEnum.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/UnsupportedFieldTypeMessage.proto -otestdata/UnsupportedFieldTypeMessage.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/UnsupportedFieldTypePrimitive.proto -otestdata/UnsupportedFieldTypePrimitive.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/UnsupportedValueTypeInAPA.proto -otestdata/UnsupportedValueTypeInAPA.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/WrongPkgName.proto -otestdata/WrongPkgName.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/UnsupportedTypeInAPAOutTemplate.proto -otestdata/UnsupportedTypeInAPAOutTemplate.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/BasicTopLevelFields.proto testdata/SimpleTemplate.proto -otestdata/twoTemplatesError.descriptor -I.

package modelgen

import (
	"fmt"
	"io/ioutil"
	"reflect"
	"strings"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"

	descriptor2 "istio.io/istio/mixer/pkg/protobuf/descriptor"
)

func TestErrorInTemplate(t *testing.T) {
	tests := []struct {
		src           string
		expectedError []string
	}{
		{"testdata/MissingPackageName.descriptor", []string{"package name missing"}},
		{"testdata/MissingBothRequiredExt.descriptor", []string{"There has to be one proto file that has the " +
			"extension istio.mixer.adapter.model.v1beta1.template_variety"}},
		{"testdata/MissingTemplateMessage.descriptor", []string{"message 'Template' not defined"}},
		{"testdata/ReservedFieldInTemplate.descriptor", []string{"testdata/ReservedFieldInTemplate.proto:28: Template message " +
			"must not contain the reserved field name 'Name'"}},
		{"testdata/Proto2BadSyntax.descriptor", []string{"testdata/Proto2BadSyntax.proto:15: Only proto3 template files are allowed."}},
		{"testdata/UnsupportedFieldTypePrimitive.descriptor", []string{"unsupported type for field 'o'. " +
			"Supported types are 'istio.policy.v1beta1.Value, string, int64, double, bool, other messages " +
			"defined within the same package, map<string, any of the listed supported types>"}},
		{"testdata/UnsupportedMapKey.descriptor", []string{"unsupported type for field 'o'."}},
		{"testdata/UnsupportedMapVal.descriptor", []string{"unsupported type for field 'o'."}},
		{"testdata/UnsupportedFieldTypeEnum.descriptor", []string{"unsupported type for field 'o'."}},
		{"testdata/WrongPkgName.descriptor", []string{"testdata/WrongPkgName.proto:16: the last segment of package " +
			"name 'foo.badStrNumbersNotAllowed123' must match the regex '^[a-zA-Z]+$'"}},
		{"testdata/UnsupportedValueTypeInAPA.descriptor", []string{"testdata/UnsupportedValueTypeInAPA.proto:26: " +
			"unsupported type for field 'o'. Supported types are 'string, int64, double, bool, other messages defined " +
			"within the same package, map<string, any of the listed supported types>'.",
			"testdata/UnsupportedValueTypeInAPA.proto: message 'OutputTemplate' not defined."}},
		{"testdata/UnsupportedTypeInAPAOutTemplate.descriptor", []string{"message 'OutputTemplate' " +
			"field 's' is of type 'foo.bar.submsg'. Only supported types in OutputTemplate message are : [string, int64," +
			" double, bool, google.protobuf.Duration, google.protobuf.TimeStamp, map<string, string>]"}},
		{"testdata/twoTemplatesError.descriptor", []string{"Proto files testdata/SimpleTemplate.proto" +
			" and testdata/BasicTopLevelFields.proto, both have the option "}},
		{"testdata/noSourceInfoNoImportsBadProto.descriptor", []string{"testdata/Proto2BadSyntax.proto:" +
			" Only proto3 template files are allowed."}},
		{"testdata/BadExplicitTemplateName.descriptor", []string{"BadExplicitTemplateName.proto: the" +
			" template_name option 'bad123' must match the regex '^[a-zA-Z]+$'"}},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.src), func(t *testing.T) {
			_, err := createTestModel(t, tt.src)

			if err == nil {
				t.Fatalf("CreateModel(%s) caused error 'nil', \n wanted err that contains strings `%v`",
					tt.src, tt.expectedError)
			} else {
				for _, e := range tt.expectedError {
					if !strings.Contains(err.Error(), e) {
						t.Errorf("CreateModel(%s) caused error\n%v;wanted err that contains string\n%s",
							tt.src, err, e)
					}
				}
			}
		})
	}
}

func TestBasicTopLevelFields(t *testing.T) {
	testFilename := "testdata/BasicTopLevelFields.descriptor"
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

func TestExplicitTmplName(t *testing.T) {
	testFilename := "testdata/ExplicitTemplateName.descriptor"
	model, err := createTestModel(t,
		testFilename)
	if err != nil {
		t.Fatalf("model creation failed %v", err)
	}
	if model.GoPackageName != "foo_listchecker" {
		t.Errorf("CreateModel(%s).PackageName = %v, wanted %s", testFilename, model.GoPackageName, "foo_listchecker")
	}
	if model.InterfaceName != "ExplicitListChecker" {
		t.Errorf("CreateModel(%s).InterfaceName = %v, wanted %s", testFilename, model.InterfaceName, "ListChecker")
	}
	if model.TemplateName != "explicitlistchecker" {
		t.Errorf("CreateModel(%s).TemplateName = %v, wanted %s", testFilename, model.TemplateName, "listchecker")
	}
	if model.VarietyName != "TEMPLATE_VARIETY_CHECK" {
		t.Errorf("CreateModel(%s).VarietyName = %v, wanted %s", testFilename, model.VarietyName, "TEMPLATE_VARIETY_CHECK")
	}
}

func TestTypeFields(t *testing.T) {
	model, err := createTestModel(t,
		"testdata/SimpleTemplate.descriptor")

	if err != nil {
		t.Fatal(err)
	}
	testSimpleTemplateFields(model.TemplateMessage, t)
	var res3MsgInfo MessageInfo
	for _, j := range model.ResourceMessages {
		if j.Name == "Resource3" {
			res3MsgInfo = j
		}
	}
	testSimpleTemplateFields(res3MsgInfo, t)
}

func TestTypeFieldsMultiFile(t *testing.T) {
	model, err := createTestModel(t,
		"testdata/multiFile.descriptor")

	if err != nil {
		t.Fatal(err)
	}
	testSimpleTemplateFields(model.TemplateMessage, t)
	var res3MsgInfo MessageInfo
	for _, j := range model.ResourceMessages {
		if j.Name == "Resource3" {
			res3MsgInfo = j
		}
	}
	testSimpleTemplateFields(res3MsgInfo, t)
}

func TestApaFields(t *testing.T) {
	model, _ := createTestModel(t,
		"testdata/SimpleApaTemplate.descriptor")

	testApaSimpleTemplateFields(model.TemplateMessage, t)
	testApaSimpleTemplateFields(model.OutputTemplateMessage, t)
}

func testApaSimpleTemplateFields(msgInfo MessageInfo, t *testing.T) {
	testFileName := "testdata/simple_apa_template"
	if len(msgInfo.Fields) != 7 {
		t.Fatalf("len(CreateModel(%s).TypeMessage.Fields) = %v, wanted %d", testFileName, len(msgInfo.Fields), 12)
	}
	testField(t, testFileName, msgInfo,
		"int64Primitive", TypeInfo{Name: "int64"}, "Int64Primitive", TypeInfo{Name: "int64"}, "")

	// No need to test all the fields since the code is the same as any other variety of template, which is already testing inside other tests.
}

func testSimpleTemplateFields(msgInfo MessageInfo, t *testing.T) {
	testFileName := "testdata/simple_template"
	if len(msgInfo.Fields) != 12 {
		t.Fatalf("len(CreateModel(%s).TypeMessage.Fields) = %v, wanted %d", testFileName, len(msgInfo.Fields), 12)
	}
	testField(t, testFileName, msgInfo,
		"blacklist", TypeInfo{Name: "bool"}, "Blacklist", TypeInfo{Name: "bool"}, "multi line comment line 2")
	testField(t, testFileName, msgInfo,
		"fieldInt64", TypeInfo{Name: "int64"},
		"FieldInt64", TypeInfo{Name: "int64"}, "")
	testField(t, testFileName, msgInfo,
		"fieldString", TypeInfo{Name: "string"},
		"FieldString", TypeInfo{Name: "string"}, "")
	testField(t, testFileName, msgInfo,
		"fieldDouble", TypeInfo{Name: "double"},
		"FieldDouble", TypeInfo{Name: "float64"}, "")
	testField(t, testFileName, msgInfo,
		"val",
		TypeInfo{Name: "istio.policy.v1beta1.Value", IsValueType: true, Import: "policy/v1beta1/type.proto"}, "Val",
		TypeInfo{Name: "interface{}", IsValueType: true}, "single line block comment")
	testField(t, testFileName, msgInfo,
		"dimensions",
		TypeInfo{Name: "map<string, istio.policy.v1beta1.Value>",
			IsMap:    true,
			Import:   "policy/v1beta1/type.proto",
			MapKey:   &TypeInfo{Name: "string"},
			MapValue: &TypeInfo{Name: "istio.policy.v1beta1.Value", IsValueType: true, Import: "policy/v1beta1/type.proto"},
		},
		"Dimensions",
		TypeInfo{
			Name:     "map[string]interface{}",
			IsMap:    true,
			MapKey:   &TypeInfo{Name: "string"},
			MapValue: &TypeInfo{Name: "interface{}", IsValueType: true},
		}, "single line comment")
	testField(t, testFileName, msgInfo,
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
	testField(t, testFileName, msgInfo,
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
	testField(t, testFileName, msgInfo,
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
	testField(t, testFileName, msgInfo,
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
	testField(t, testFileName, msgInfo,
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
	testField(t, testFileName, msgInfo,
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

func testField(t *testing.T, testFilename string, msgInfo MessageInfo, protoFldName string, protoFldType TypeInfo,
	goFldName string, goFldType TypeInfo, comment string) {
	found := false
	for _, cf := range msgInfo.Fields {
		if cf.ProtoName == protoFldName {
			found = true
			if cf.GoName != goFldName ||
				!reflect.DeepEqual(cf.ProtoType, protoFldType) ||
				!reflect.DeepEqual(cf.GoType, goFldType) ||
				!strings.Contains(cf.Comment, comment) {
				t.Fatalf("Got CreateModel(%s).%s.Fields[%s] = \nGoName:%s, ProtoType:%v, GoType:%v, Comment:%s"+
					";wanted\nGoName:%s, ProtoType:%v, GoType:%v, comment: %s",
					testFilename, msgInfo.Name, protoFldName, cf.GoName, cf.ProtoType, cf.GoType, cf.Comment, goFldName, protoFldType, goFldType, comment)
			}
		}
	}
	if !found {
		t.Fatalf("CreateModel(%s).TemplateMessage = %v, wanted to contain field with name '%s'", testFilename, msgInfo.Fields, protoFldName)
	}
}

func createTestModel(t *testing.T, inputFDS string) (*Model, error) {
	fds, err := getFileDescSet(inputFDS)
	if err != nil {
		t.Fatalf("Unable to parse file descriptor set file %v", err)

	}

	parser := descriptor2.CreateFileDescriptorSetParser(fds, map[string]string{}, "")
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
