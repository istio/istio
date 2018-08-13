package tests

import (
	"github.com/modern-go/reflect2"
	"testing"
)

func Test_struct_eface(t *testing.T) {
	type TestObject struct {
		Field1 interface{}
	}
	var pEFace = func(val interface{}) interface{} {
		return &val
	}
	t.Run("SetIndex", testOp(func(api reflect2.API) interface{} {
		valType := api.TypeOf(TestObject{}).(reflect2.StructType)
		field1 := valType.FieldByName("Field1")
		obj := TestObject{}
		field1.Set(&obj, pEFace(100))
		return obj
	}))
	t.Run("GetIndex", testOp(func(api reflect2.API) interface{} {
		obj := TestObject{Field1: 100}
		valType := api.TypeOf(obj).(reflect2.StructType)
		field1 := valType.FieldByName("Field1")
		return field1.Get(&obj)
	}))
}
