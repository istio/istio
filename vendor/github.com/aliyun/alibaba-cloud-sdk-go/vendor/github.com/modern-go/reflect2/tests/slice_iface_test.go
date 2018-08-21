package tests

import (
	"errors"
	"github.com/modern-go/reflect2"
	"github.com/modern-go/test"
	"testing"

	"context"
	"github.com/modern-go/test/must"
	"unsafe"
)

func Test_slice_iface(t *testing.T) {
	var pError = func(msg string) *error {
		err := errors.New(msg)
		return &err
	}
	t.Run("MakeSlice", testOp(func(api reflect2.API) interface{} {
		valType := api.TypeOf([]error{}).(reflect2.SliceType)
		obj := *(valType.MakeSlice(5, 10).(*[]error))
		obj[0] = errors.New("hello")
		obj[4] = errors.New("world")
		return obj
	}))
	t.Run("SetIndex", testOp(func(api reflect2.API) interface{} {
		obj := []error{errors.New("hello"), nil}
		valType := api.TypeOf(obj).(reflect2.SliceType)
		valType.SetIndex(&obj, 0, pError("hi"))
		valType.SetIndex(&obj, 1, pError("world"))
		return obj
	}))
	t.Run("UnsafeSetIndex", test.Case(func(ctx context.Context) {
		obj := []error{errors.New("hello"), nil}
		valType := reflect2.TypeOf(obj).(reflect2.SliceType)
		elem0 := errors.New("hi")
		valType.UnsafeSetIndex(reflect2.PtrOf(obj), 0, unsafe.Pointer(&elem0))
		elem1 := errors.New("world")
		valType.UnsafeSetIndex(reflect2.PtrOf(obj), 1, unsafe.Pointer(&elem1))
		must.Equal([]error{elem0, elem1}, obj)
	}))
	t.Run("GetIndex", testOp(func(api reflect2.API) interface{} {
		obj := []error{errors.New("hello"), nil}
		valType := api.TypeOf(obj).(reflect2.SliceType)
		return []interface{}{
			valType.GetIndex(&obj, 0),
			valType.GetIndex(&obj, 1),
		}
	}))
	t.Run("UnsafeGetIndex", test.Case(func(ctx context.Context) {
		obj := []error{errors.New("hello"), nil}
		valType := reflect2.TypeOf(obj).(reflect2.SliceType)
		elem0 := valType.UnsafeGetIndex(reflect2.PtrOf(obj), 0)
		must.Equal(errors.New("hello"), *(*error)(elem0))
	}))
	t.Run("Append", testOp(func(api reflect2.API) interface{} {
		obj := make([]error, 2, 3)
		obj[0] = errors.New("1")
		obj[1] = errors.New("2")
		valType := api.TypeOf(obj).(reflect2.SliceType)
		ptr := &obj
		valType.Append(ptr, pError("3"))
		// will trigger grow
		valType.Append(ptr, pError("4"))
		return ptr
	}))
	t.Run("UnsafeAppend", test.Case(func(ctx context.Context) {
		obj := make([]error, 2, 3)
		obj[0] = errors.New("1")
		obj[1] = errors.New("2")
		valType := reflect2.TypeOf(obj).(reflect2.SliceType)
		ptr := reflect2.PtrOf(obj)
		elem2 := errors.New("3")
		valType.UnsafeAppend(ptr, unsafe.Pointer(&elem2))
		elem3 := errors.New("4")
		valType.UnsafeAppend(ptr, unsafe.Pointer(&elem3))
		must.Equal(&[]error{
			obj[0], obj[1], elem2, elem3,
		}, valType.PackEFace(ptr))
	}))
}
