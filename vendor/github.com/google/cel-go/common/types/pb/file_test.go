package pb

import (
	testpb "github.com/google/cel-go/test/proto3pb"
	"testing"
)

func TestFileDescription_GetTypes(t *testing.T) {
	fd, err := DescribeFile(&testpb.TestAllTypes{})
	if err != nil {
		t.Error(err)
	}
	expected := []string{
		"google.expr.proto3.test.TestAllTypes",
		"google.expr.proto3.test.TestAllTypes.NestedMessage",
		"google.expr.proto3.test.TestAllTypes.MapStringStringEntry",
		"google.expr.proto3.test.TestAllTypes.MapInt64NestedTypeEntry",
		"google.expr.proto3.test.NestedTestAllTypes"}
	if len(fd.GetTypeNames()) != len(expected) {
		t.Errorf("got '%v', wanted '%v'", fd.GetTypeNames(), expected)
	}
	for _, tn := range fd.GetTypeNames() {
		var found = false
		for _, elem := range expected {
			if elem == tn {
				found = true
				break
			}
		}
		if !found {
			t.Error("Unexpected type name", tn)
		}
	}
	for _, typeName := range fd.GetTypeNames() {
		td, err := fd.GetTypeDescription(typeName)
		if err != nil {
			t.Error(err)
		}
		if td.Name() != typeName {
			t.Error("Indexed type name not equal to descriptor type name", td, typeName)
		}
		if td.file != fd {
			t.Error("Indexed type does not refer to current file", td)
		}
	}
}

func TestFileDescription_GetEnumNames(t *testing.T) {
	fd, err := DescribeFile(&testpb.TestAllTypes{})
	if err != nil {
		t.Error(err)
	}
	expected := map[string]int32{
		"google.expr.proto3.test.TestAllTypes.NestedEnum.FOO": 0,
		"google.expr.proto3.test.TestAllTypes.NestedEnum.BAR": 1,
		"google.expr.proto3.test.TestAllTypes.NestedEnum.BAZ": 2,
		"google.expr.proto3.test.GlobalEnum.GOO":              0,
		"google.expr.proto3.test.GlobalEnum.GAR":              1,
		"google.expr.proto3.test.GlobalEnum.GAZ":              2}
	if len(expected) != len(fd.GetEnumNames()) {
		t.Error("Count of enum names does not match expected'",
			fd.GetEnumNames(), expected)
	}
	for _, enumName := range fd.GetEnumNames() {
		if enumVal, found := expected[enumName]; found {
			ed, err := fd.GetEnumDescription(enumName)
			if err != nil {
				t.Error(err)
			} else if ed.Value() != enumVal {
				t.Errorf("Enum did not have expected value. %s got '%v', wanted '%v'",
					enumName, ed.Value(), enumVal)
			}
		} else {
			t.Errorf("Enum value not found for: %s", enumName)
		}
	}
}
