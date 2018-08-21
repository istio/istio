package tests

import (
	"github.com/modern-go/reflect2"
	"github.com/modern-go/test"
	"testing"

	"context"
	"github.com/modern-go/test/must"
	"unsafe"
)

func Test_slice_ptr(t *testing.T) {
	var pInt = func(val int) *int {
		return &val
	}
	t.Run("MakeSlice", testOp(func(api reflect2.API) interface{} {
		valType := api.TypeOf([]*int{}).(reflect2.SliceType)
		obj := valType.MakeSlice(5, 10)
		(*obj.(*[]*int))[0] = pInt(1)
		(*obj.(*[]*int))[4] = pInt(5)
		return obj
	}))
	t.Run("SetIndex", testOp(func(api reflect2.API) interface{} {
		obj := []*int{pInt(1), nil}
		valType := api.TypeOf(obj).(reflect2.SliceType)
		elem1 := pInt(2)
		valType.SetIndex(&obj, 0, &elem1)
		elem2 := pInt(3)
		valType.SetIndex(&obj, 1, &elem2)
		return obj
	}))
	t.Run("UnsafeSetIndex", test.Case(func(ctx context.Context) {
		obj := []*int{pInt(1), nil}
		valType := reflect2.TypeOf(obj).(reflect2.SliceType)
		elem1 := pInt(2)
		valType.UnsafeSetIndex(unsafe.Pointer(&obj), 0, unsafe.Pointer(&elem1))
		elem2 := pInt(1)
		valType.UnsafeSetIndex(unsafe.Pointer(&obj), 1, unsafe.Pointer(&elem2))
		must.Equal([]*int{pInt(2), pInt(1)}, obj)
	}))
	t.Run("GetIndex", testOp(func(api reflect2.API) interface{} {
		obj := []*int{pInt(1), nil}
		valType := api.TypeOf(obj).(reflect2.SliceType)
		return []interface{}{
			valType.GetIndex(&obj, 0),
			valType.GetIndex(&obj, 1),
		}
	}))
	t.Run("Append", testOp(func(api reflect2.API) interface{} {
		obj := make([]*int, 2, 3)
		obj[0] = pInt(1)
		obj[1] = pInt(2)
		valType := api.TypeOf(obj).(reflect2.SliceType)
		elem1 := pInt(3)
		valType.Append(&obj, &elem1)
		// will trigger grow
		elem2 := pInt(4)
		valType.Append(&obj, &elem2)
		return obj
	}))
}
