package tests

import (
	"github.com/modern-go/reflect2"
	"github.com/modern-go/test"
	"testing"

	"context"
	"github.com/modern-go/test/must"
)

func Test_slice(t *testing.T) {
	var pInt = func(val int) *int {
		return &val
	}
	t.Run("New", testOp(func(api reflect2.API) interface{} {
		valType := api.TypeOf([]int{})
		obj := *valType.New().(*[]int)
		obj = append(obj, 1)
		return obj
	}))
	t.Run("IsNil", testOp(func(api reflect2.API) interface{} {
		valType := api.TypeOf([]int{})
		var nilSlice []int
		s := []int{}
		return []interface{}{
			valType.IsNil(&nilSlice),
			valType.IsNil(&s),
			valType.IsNil(nil),
		}
	}))
	t.Run("SetNil", testOp(func(api reflect2.API) interface{} {
		valType := api.TypeOf([]int{}).(reflect2.SliceType)
		s := []int{1}
		valType.SetNil(&s)
		return s
	}))
	t.Run("Set", testOp(func(api reflect2.API) interface{} {
		valType := api.TypeOf([]int{}).(reflect2.SliceType)
		s1 := []int{1}
		s2 := []int{2}
		valType.Set(&s1, &s2)
		return s1
	}))
	t.Run("MakeSlice", testOp(func(api reflect2.API) interface{} {
		valType := api.TypeOf([]int{}).(reflect2.SliceType)
		obj := *(valType.MakeSlice(5, 10).(*[]int))
		obj[0] = 100
		obj[4] = 20
		return obj
	}))
	t.Run("UnsafeMakeSlice", test.Case(func(ctx context.Context) {
		valType := reflect2.TypeOf([]int{}).(reflect2.SliceType)
		obj := valType.UnsafeMakeSlice(5, 10)
		must.Equal(&[]int{0, 0, 0, 0, 0}, valType.PackEFace(obj))
	}))
	t.Run("SetIndex", testOp(func(api reflect2.API) interface{} {
		obj := []int{1, 2}
		valType := api.TypeOf(obj).(reflect2.SliceType)
		valType.SetIndex(&obj, 0, pInt(100))
		valType.SetIndex(&obj, 1, pInt(20))
		return obj
	}))
	t.Run("UnsafeSetIndex", test.Case(func(ctx context.Context) {
		obj := []int{1, 2}
		valType := reflect2.TypeOf(obj).(reflect2.SliceType)
		valType.UnsafeSetIndex(reflect2.PtrOf(obj), 0, reflect2.PtrOf(100))
		valType.UnsafeSetIndex(reflect2.PtrOf(obj), 1, reflect2.PtrOf(10))
		must.Equal([]int{100, 10}, obj)
	}))
	t.Run("GetIndex", testOp(func(api reflect2.API) interface{} {
		obj := []int{1, 2}
		valType := api.TypeOf(obj).(reflect2.SliceType)
		return []interface{}{
			valType.GetIndex(&obj, 1).(*int),
		}
	}))
	t.Run("UnsafeGetIndex", test.Case(func(ctx context.Context) {
		obj := []int{1, 2}
		valType := reflect2.TypeOf(obj).(reflect2.SliceType)
		elem0 := valType.UnsafeGetIndex(reflect2.PtrOf(obj), 0)
		must.Equal(1, *(*int)(elem0))
		elem1 := valType.UnsafeGetIndex(reflect2.PtrOf(obj), 1)
		must.Equal(2, *(*int)(elem1))
	}))
	t.Run("Append", testOp(func(api reflect2.API) interface{} {
		obj := make([]int, 2, 3)
		obj[0] = 1
		obj[1] = 2
		valType := api.TypeOf(obj).(reflect2.SliceType)
		ptr := &obj
		valType.Append(ptr, pInt(3))
		// will trigger grow
		valType.Append(ptr, pInt(4))
		return ptr
	}))
	t.Run("UnsafeAppend", test.Case(func(ctx context.Context) {
		obj := make([]int, 2, 3)
		obj[0] = 1
		obj[1] = 2
		valType := reflect2.TypeOf(obj).(reflect2.SliceType)
		ptr := reflect2.PtrOf(obj)
		valType.UnsafeAppend(ptr, reflect2.PtrOf(3))
		valType.UnsafeAppend(ptr, reflect2.PtrOf(4))
		must.Equal(&[]int{1, 2, 3, 4}, valType.PackEFace(ptr))
	}))
	t.Run("Grow", testOp(func(api reflect2.API) interface{} {
		obj := make([]int, 2, 3)
		obj[0] = 1
		obj[1] = 2
		valType := reflect2.TypeOf(obj).(reflect2.SliceType)
		valType.Grow(&obj, 4)
		return obj
	}))
}
