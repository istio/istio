package pb

import (
	"testing"

	"github.com/golang/protobuf/proto"

	testpb "github.com/google/cel-go/test/proto3pb"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

func TestTypeDescription_FieldCount(t *testing.T) {
	td, err := DescribeValue(&testpb.NestedTestAllTypes{})
	if err != nil {
		t.Error(err)
	}
	if td.FieldCount() != 2 {
		t.Errorf("Unexpected field count. got '%d', wanted '%d'",
			td.FieldCount(), 2)
	}
}

func TestTypeDescription_Any(t *testing.T) {
	_, err := DescribeType(".google.protobuf.Any")
	if err != nil {
		t.Error(err)
	}
}

func TestTypeDescription_Json(t *testing.T) {
	_, err := DescribeType(".google.protobuf.Value")
	if err != nil {
		t.Error(err)
	}
}

func TestTypeDescription_JsonNotInTypeInit(t *testing.T) {
	_, err := DescribeType(".google.protobuf.ListValue")
	if err != nil {
		t.Error(err)
	}
}

func TestTypeDescription_Wrapper(t *testing.T) {
	_, err := DescribeType(".google.protobuf.BoolValue")
	if err != nil {
		t.Error(err)
	}
}

func TestTypeDescription_WrapperNotInTypeInit(t *testing.T) {
	_, err := DescribeType(".google.protobuf.BytesValue")
	if err != nil {
		t.Error(err)
	}
}

func TestTypeDescription_Field(t *testing.T) {
	td, err := DescribeValue(&testpb.NestedTestAllTypes{})
	if err != nil {
		t.Error(err)
	}
	fd, found := td.FieldByName("payload")
	if !found {
		t.Error("Field 'payload' not found")
	}
	if fd.OrigName() != "payload" {
		t.Error("Unexpected proto name for field 'payload'", fd.OrigName())
	}
	if fd.Name() != "Payload" {
		t.Error("Unexpected struct name for field 'payload'", fd.Name())
	}
	if fd.GetterName() != "GetPayload" {
		t.Error("Unexpected accessor name for field 'payload'", fd.GetterName())
	}
	if fd.IsOneof() {
		t.Error("Field payload is listed as a oneof and it is not.")
	}
	if fd.IsMap() {
		t.Error("Field 'payload' is listed as a map and it is not.")
	}
	if !fd.IsMessage() {
		t.Error("Field 'payload' is not marked as a message.")
	}
	if fd.IsEnum() {
		t.Error("Field 'payload' is marked as an enum.")
	}
	if fd.IsRepeated() {
		t.Error("Field 'payload' is marked as repeated.")
	}
	if fd.Index() != 1 {
		t.Error("Field 'payload' was fetched at index 1, but not listed there.")
	}
	if !proto.Equal(fd.CheckedType(), &exprpb.Type{
		TypeKind: &exprpb.Type_MessageType{
			MessageType: "google.expr.proto3.test.TestAllTypes"}}) {
		t.Error("Field 'payload' had an unexpected checked type.")
	}
}
