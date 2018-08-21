package tests

import (
	"github.com/modern-go/reflect2"
	"github.com/modern-go/test"
	"testing"

	"context"
	"github.com/modern-go/test/must"
	"github.com/modern-go/test/should"
	"unsafe"
)

func Test_slice_eface(t *testing.T) {
	t.Run("MakeSlice", testOp(func(api reflect2.API) interface{} {
		valType := api.TypeOf([]interface{}{}).(reflect2.SliceType)
		obj := valType.MakeSlice(5, 10)
		(*obj.(*[]interface{}))[0] = 100
		(*obj.(*[]interface{}))[4] = 20
		return obj
	}))
	t.Run("SetIndex", testOp(func(api reflect2.API) interface{} {
		obj := []interface{}{1, nil}
		valType := api.TypeOf(obj).(reflect2.SliceType)
		elem0 := interface{}(100)
		valType.SetIndex(&obj, 0, &elem0)
		elem1 := interface{}(20)
		valType.SetIndex(&obj, 1, &elem1)
		return obj
	}))
	t.Run("UnsafeSetIndex", test.Case(func(ctx context.Context) {
		obj := []interface{}{1, 2}
		valType := reflect2.TypeOf(obj).(reflect2.SliceType)
		var elem0 interface{} = 100
		valType.UnsafeSetIndex(reflect2.PtrOf(obj), 0, unsafe.Pointer(&elem0))
		var elem1 interface{} = 10
		valType.UnsafeSetIndex(reflect2.PtrOf(obj), 1, unsafe.Pointer(&elem1))
		must.Equal([]interface{}{100, 10}, obj)
	}))
	t.Run("GetIndex", testOp(func(api reflect2.API) interface{} {
		obj := []interface{}{1, nil}
		valType := api.TypeOf(obj).(reflect2.SliceType)
		return []interface{}{
			valType.GetIndex(&obj, 0),
			valType.GetIndex(&obj, 1),
		}
	}))
	t.Run("UnsafeGetIndex", test.Case(func(ctx context.Context) {
		obj := []interface{}{1, nil}
		valType := reflect2.TypeOf(obj).(reflect2.SliceType)
		elem0 := valType.UnsafeGetIndex(unsafe.Pointer(&obj), 0)
		must.Equal(1, *(*interface{})(elem0))
	}))
	t.Run("Append", testOp(func(api reflect2.API) interface{} {
		obj := make([]interface{}, 2, 3)
		obj[0] = 1
		obj[1] = 2
		valType := api.TypeOf(obj).(reflect2.SliceType)
		elem1 := interface{}(3)
		valType.Append(&obj, &elem1)
		// will trigger grow
		elem2 := interface{}(4)
		valType.Append(&obj, &elem2)
		return obj
	}))
	t.Run("UnsafeAppend", test.Case(func(ctx context.Context) {
		obj := make([]interface{}, 2, 3)
		obj[0] = 1
		obj[1] = 2
		valType := reflect2.TypeOf(obj).(reflect2.SliceType)
		var elem2 interface{} = 3
		valType.UnsafeAppend(unsafe.Pointer(&obj), unsafe.Pointer(&elem2))
		var elem3 interface{} = 4
		valType.UnsafeAppend(unsafe.Pointer(&obj), unsafe.Pointer(&elem3))
		should.Equal([]interface{}{1, 2, 3, 4}, valType.UnsafeIndirect(unsafe.Pointer(&obj)))
	}))
}
