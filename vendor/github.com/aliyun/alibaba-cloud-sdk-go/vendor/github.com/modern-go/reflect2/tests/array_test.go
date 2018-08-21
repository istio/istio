package tests

import (
	"github.com/modern-go/reflect2"
	"testing"
)

func Test_array(t *testing.T) {
	var pInt = func(val int) *int {
		return &val
	}
	t.Run("New", testOp(func(api reflect2.API) interface{} {
		valType := api.TypeOf([2]int{})
		obj := valType.New()
		(*(obj.(*[2]int)))[0] = 100
		(*(obj.(*[2]int)))[1] = 200
		return obj
	}))
	t.Run("Indirect", testOp(func(api reflect2.API) interface{} {
		valType := api.TypeOf([2]int{})
		return valType.Indirect(&[2]int{})
	}))
	t.Run("SetIndex", testOp(func(api reflect2.API) interface{} {
		obj := [2]int{}
		valType := api.TypeOf(obj).(reflect2.ArrayType)
		valType.SetIndex(&obj, 0, pInt(100))
		valType.SetIndex(&obj, 1, pInt(200))
		return obj
	}))
	t.Run("GetIndex", testOp(func(api reflect2.API) interface{} {
		obj := [2]int{1, 2}
		valType := api.TypeOf(obj).(reflect2.ArrayType)
		return []interface{}{
			valType.GetIndex(&obj, 0),
			valType.GetIndex(&obj, 1),
		}
	}))
}
