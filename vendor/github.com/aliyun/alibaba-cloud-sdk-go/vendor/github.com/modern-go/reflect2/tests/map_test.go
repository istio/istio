package tests

import (
	"github.com/modern-go/reflect2"
	"github.com/modern-go/test/must"
	"testing"

	"context"
	"github.com/modern-go/test"
	"reflect"
	"unsafe"
)

func Test_map(t *testing.T) {
	var pInt = func(val int) *int {
		return &val
	}
	t.Run("New", testOp(func(api reflect2.API) interface{} {
		valType := api.TypeOf(map[int]int{})
		m := valType.New().(*map[int]int)
		return m
	}))
	t.Run("IsNil", testOp(func(api reflect2.API) interface{} {
		valType := api.TypeOf(map[int]int{})
		var nilMap map[int]int
		m := map[int]int{}
		return []interface{}{
			valType.IsNil(&nilMap),
			valType.IsNil(&m),
		}
	}))
	t.Run("MakeMap", testOp(func(api reflect2.API) interface{} {
		valType := api.TypeOf(map[int]int{}).(reflect2.MapType)
		m := *(valType.MakeMap(0).(*map[int]int))
		m[2] = 4
		m[3] = 9
		return m
	}))
	t.Run("UnsafeMakeMap", test.Case(func(ctx context.Context) {
		valType := reflect2.TypeOf(map[int]int{}).(reflect2.MapType)
		m := *(*map[int]int)(valType.UnsafeMakeMap(0))
		m[2] = 4
		m[3] = 9
	}))
	t.Run("PackEFace", test.Case(func(ctx context.Context) {
		valType := reflect2.TypeOf(map[int]int{}).(reflect2.MapType)
		m := valType.UnsafeMakeMap(0)
		must.Equal(&map[int]int{}, valType.PackEFace(unsafe.Pointer(m)))
	}))
	t.Run("Indirect", testOp(func(api reflect2.API) interface{} {
		valType := reflect2.TypeOf(map[int]int{})
		return valType.Indirect(&map[int]int{})
	}))
	t.Run("SetIndex", testOp(func(api reflect2.API) interface{} {
		obj := map[int]int{}
		valType := api.TypeOf(obj).(reflect2.MapType)
		valType.SetIndex(&obj, pInt(2), pInt(4))
		valType.SetIndex(&obj, pInt(3), pInt(9))
		must.Equal(4, obj[2])
		return obj
	}))
	t.Run("UnsafeSetIndex", test.Case(func(ctx context.Context) {
		obj := map[int]int{}
		valType := reflect2.TypeOf(obj).(reflect2.MapType)
		valType.UnsafeSetIndex(unsafe.Pointer(&obj), reflect2.PtrOf(2), reflect2.PtrOf(4))
		must.Equal(map[int]int{2: 4}, obj)
	}))
	t.Run("GetIndex", testOp(func(api reflect2.API) interface{} {
		obj := map[int]int{3: 9, 2: 4}
		valType := api.TypeOf(obj).(reflect2.MapType)
		return []interface{}{
			*valType.GetIndex(&obj, pInt(3)).(*int),
			valType.GetIndex(&obj, pInt(0)).(*int),
		}
	}))
	t.Run("UnsafeGetIndex", test.Case(func(ctx context.Context) {
		obj := map[int]int{3: 9, 2: 4}
		valType := reflect2.TypeOf(obj).(reflect2.MapType)
		elem := valType.UnsafeGetIndex(unsafe.Pointer(&obj), reflect2.PtrOf(3))
		must.Equal(9, *(*int)(elem))
	}))
	t.Run("Iterate", testOp(func(api reflect2.API) interface{} {
		obj := map[int]int{2: 4}
		valType := api.TypeOf(obj).(reflect2.MapType)
		iter := valType.Iterate(&obj)
		must.Pass(iter.HasNext(), "api", api)
		key1, elem1 := iter.Next()
		must.Pass(!iter.HasNext(), "api", api)
		return []interface{}{key1, elem1}
	}))
	t.Run("UnsafeIterate", test.Case(func(ctx context.Context) {
		obj := map[int]int{2: 4}
		valType := reflect2.TypeOf(obj).(reflect2.MapType)
		iter := valType.UnsafeIterate(unsafe.Pointer(&obj))
		must.Pass(iter.HasNext())
		key, elem := iter.UnsafeNext()
		must.Equal(2, *(*int)(key))
		must.Equal(4, *(*int)(elem))
	}))
}

func Benchmark_map_unsafe(b *testing.B) {
	obj := map[int]int{}
	valType := reflect2.TypeOf(obj).(*reflect2.UnsafeMapType)
	m := unsafe.Pointer(&obj)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		valType.UnsafeSetIndex(m, reflect2.PtrOf(2), reflect2.PtrOf(4))
	}
}

func Benchmark_map_safe(b *testing.B) {
	obj := map[int]int{}
	val := reflect.ValueOf(obj)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		val.SetMapIndex(reflect.ValueOf(2), reflect.ValueOf(4))
	}
}
