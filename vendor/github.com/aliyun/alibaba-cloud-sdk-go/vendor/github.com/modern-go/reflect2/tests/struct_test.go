package tests

import (
	"github.com/modern-go/reflect2"
	"github.com/modern-go/test"
	"testing"

	"context"
	"github.com/modern-go/test/must"
	"unsafe"
)

func Test_struct(t *testing.T) {
	type TestObject struct {
		Field1 int
		Field2 int
	}
	var pInt = func(val int) *int {
		return &val
	}
	t.Run("New", testOp(func(api reflect2.API) interface{} {
		valType := api.TypeOf(TestObject{})
		obj := valType.New()
		obj.(*TestObject).Field1 = 20
		obj.(*TestObject).Field2 = 100
		return obj
	}))
	t.Run("PackEFace", test.Case(func(ctx context.Context) {
		valType := reflect2.TypeOf(TestObject{})
		ptr := valType.UnsafeNew()
		must.Equal(&TestObject{}, valType.PackEFace(ptr))
	}))
	t.Run("Indirect", test.Case(func(ctx context.Context) {
		valType := reflect2.TypeOf(TestObject{})
		must.Equal(TestObject{}, valType.Indirect(&TestObject{}))
	}))
	t.Run("SetIndex", testOp(func(api reflect2.API) interface{} {
		valType := api.TypeOf(TestObject{}).(reflect2.StructType)
		field1 := valType.FieldByName("Field1")
		obj := TestObject{}
		field1.Set(&obj, pInt(100))
		return obj
	}))
	t.Run("UnsafeSetIndex", test.Case(func(ctx context.Context) {
		valType := reflect2.TypeOf(TestObject{}).(reflect2.StructType)
		field1 := valType.FieldByName("Field1")
		obj := TestObject{}
		field1.UnsafeSet(unsafe.Pointer(&obj), reflect2.PtrOf(100))
		must.Equal(100, obj.Field1)
	}))
	t.Run("GetIndex", testOp(func(api reflect2.API) interface{} {
		obj := TestObject{Field1: 100}
		valType := api.TypeOf(obj).(reflect2.StructType)
		field1 := valType.FieldByName("Field1")
		return field1.Get(&obj)
	}))
	t.Run("UnsafeGetIndex", test.Case(func(ctx context.Context) {
		obj := TestObject{Field1: 100}
		valType := reflect2.TypeOf(obj).(reflect2.StructType)
		field1 := valType.FieldByName("Field1")
		value := field1.UnsafeGet(unsafe.Pointer(&obj))
		must.Equal(100, *(*int)(value))
	}))
}
