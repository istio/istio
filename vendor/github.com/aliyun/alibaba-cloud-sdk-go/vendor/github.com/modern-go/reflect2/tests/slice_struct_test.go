package tests

import (
	"github.com/modern-go/reflect2"
	"testing"
)

func Test_slice_struct(t *testing.T) {
	var pInt = func(val int) *int {
		return &val
	}
	t.Run("SetIndex", testOp(func(api reflect2.API) interface{} {
		type TestObject struct {
			Field1 float64
			Field2 float64
		}
		obj := []TestObject{{}, {}}
		valType := api.TypeOf(obj).(reflect2.SliceType)
		valType.SetIndex(&obj, 0, &TestObject{1, 3})
		valType.SetIndex(&obj, 1, &TestObject{2, 4})
		return obj
	}))
	t.Run("SetIndex single ptr struct", testOp(func(api reflect2.API) interface{} {
		type TestObject struct {
			Field1 *int
		}
		obj := []TestObject{{}, {}}
		valType := api.TypeOf(obj).(reflect2.SliceType)
		valType.SetIndex(&obj, 0, &TestObject{pInt(1)})
		valType.SetIndex(&obj, 1, &TestObject{pInt(2)})
		return obj
	}))
	t.Run("SetIndex single chan struct", testOp(func(api reflect2.API) interface{} {
		type TestObject struct {
			Field1 chan int
		}
		obj := []TestObject{{}, {}}
		valType := api.TypeOf(obj).(reflect2.SliceType)
		valType.SetIndex(&obj, 0, &TestObject{})
		valType.SetIndex(&obj, 1, &TestObject{})
		return obj
	}))
	t.Run("SetIndex single func struct", testOp(func(api reflect2.API) interface{} {
		type TestObject struct {
			Field1 func()
		}
		obj := []TestObject{{}, {}}
		valType := api.TypeOf(obj).(reflect2.SliceType)
		valType.SetIndex(&obj, 0, &TestObject{})
		valType.SetIndex(&obj, 1, &TestObject{})
		return obj
	}))
}
