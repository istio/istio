package tests

import (
	"context"
	"github.com/modern-go/reflect2"
	"github.com/modern-go/test"
	"github.com/modern-go/test/must"
	"testing"
	"unsafe"
)

func Test_map_elem_bytes(t *testing.T) {
	var pInt = func(val int) *int {
		return &val
	}
	t.Run("SetIndex", testOp(func(api reflect2.API) interface{} {
		obj := map[int][]byte{}
		valType := api.TypeOf(obj).(reflect2.MapType)
		elem1 := []byte("hello")
		valType.SetIndex(&obj, pInt(2), &elem1)
		elem2 := []byte(nil)
		valType.SetIndex(&obj, pInt(3), &elem2)
		return obj
	}))
	t.Run("UnsafeSetIndex", test.Case(func(ctx context.Context) {
		obj := map[int][]byte{}
		valType := reflect2.TypeOf(obj).(reflect2.MapType)
		hello := []byte("hello")
		valType.UnsafeSetIndex(unsafe.Pointer(&obj), reflect2.PtrOf(2), unsafe.Pointer(&hello))
		elem2 := []byte(nil)
		valType.UnsafeSetIndex(unsafe.Pointer(&obj), reflect2.PtrOf(3), unsafe.Pointer(&elem2))
		must.Equal([]byte("hello"), obj[2])
		must.Nil(obj[3])
	}))
	t.Run("UnsafeGetIndex", test.Case(func(ctx context.Context) {
		obj := map[int][]byte{2: []byte("hello")}
		valType := reflect2.TypeOf(obj).(reflect2.MapType)
		elem := valType.UnsafeGetIndex(unsafe.Pointer(&obj), reflect2.PtrOf(2))
		must.Equal([]byte("hello"), valType.Elem().UnsafeIndirect(elem))
	}))
}
