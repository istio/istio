package tests

import (
	"github.com/modern-go/reflect2"
	"testing"
)

func Test_map_elem_array(t *testing.T) {
	var pInt = func(val int) *int {
		return &val
	}
	t.Run("SetIndex", testOp(func(api reflect2.API) interface{} {
		obj := map[int][2]*int{}
		valType := api.TypeOf(obj).(reflect2.MapType)
		elem1 := [2]*int{(*int)(reflect2.PtrOf(1)), (*int)(reflect2.PtrOf(2))}
		valType.SetIndex(&obj, pInt(2), &elem1)
		elem2 := [2]*int{(*int)(reflect2.PtrOf(3)), (*int)(reflect2.PtrOf(4))}
		valType.SetIndex(&obj, pInt(3), &elem2)
		return obj
	}))
	t.Run("SetIndex zero length array", testOp(func(api reflect2.API) interface{} {
		obj := map[int][0]*int{}
		valType := api.TypeOf(obj).(reflect2.MapType)
		elem1 := [0]*int{}
		valType.SetIndex(&obj, pInt(2), &elem1)
		elem2 := [0]*int{}
		valType.SetIndex(&obj, pInt(3), &elem2)
		return obj
	}))
	t.Run("SetIndex single ptr array", testOp(func(api reflect2.API) interface{} {
		obj := map[int][1]*int{}
		valType := api.TypeOf(obj).(reflect2.MapType)
		elem1 := [1]*int{(*int)(reflect2.PtrOf(1))}
		valType.SetIndex(&obj, pInt(2), &elem1)
		elem2 := [1]*int{}
		valType.SetIndex(&obj, pInt(3), &elem2)
		return obj
	}))
	t.Run("SetIndex single chan array", testOp(func(api reflect2.API) interface{} {
		obj := map[int][1]chan int{}
		valType := api.TypeOf(obj).(reflect2.MapType)
		elem1 := [1]chan int{}
		valType.SetIndex(&obj, pInt(2), &elem1)
		elem2 := [1]chan int{}
		valType.SetIndex(&obj, pInt(3), &elem2)
		return obj
	}))
	t.Run("SetIndex single func array", testOp(func(api reflect2.API) interface{} {
		obj := map[int][1]func(){}
		valType := api.TypeOf(obj).(reflect2.MapType)
		elem1 := [1]func(){}
		valType.SetIndex(&obj, pInt(2), &elem1)
		elem2 := [1]func(){}
		valType.SetIndex(&obj, pInt(3), &elem2)
		return obj
	}))
}
