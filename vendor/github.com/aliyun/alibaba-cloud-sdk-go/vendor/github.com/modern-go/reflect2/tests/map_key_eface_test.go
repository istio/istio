package tests

import (
	"github.com/modern-go/reflect2"
	"github.com/modern-go/test/must"
	"testing"
)

func Test_map_key_eface(t *testing.T) {
	var pEFace = func(val interface{}) interface{} {
		return &val
	}
	var pInt = func(val int) *int {
		return &val
	}
	t.Run("SetIndex", testOp(func(api reflect2.API) interface{} {
		obj := map[interface{}]int{}
		valType := api.TypeOf(obj).(reflect2.MapType)
		valType.SetIndex(&obj, pEFace(2), pInt(4))
		valType.SetIndex(&obj, pEFace(3), pInt(9))
		valType.SetIndex(&obj, pEFace(nil), pInt(9))
		return obj
	}))
	t.Run("GetIndex", testOp(func(api reflect2.API) interface{} {
		obj := map[interface{}]int{3: 9, 2: 4}
		valType := api.TypeOf(obj).(reflect2.MapType)
		return []interface{}{
			valType.GetIndex(&obj, pEFace(3)),
			valType.GetIndex(&obj, pEFace(0)),
			valType.GetIndex(&obj, pEFace(nil)),
			valType.GetIndex(&obj, pEFace("")),
		}
	}))
	t.Run("Iterate", testOp(func(api reflect2.API) interface{} {
		obj := map[interface{}]int{2: 4}
		valType := api.TypeOf(obj).(reflect2.MapType)
		iter := valType.Iterate(&obj)
		must.Pass(iter.HasNext(), "api", api)
		key1, elem1 := iter.Next()
		must.Pass(!iter.HasNext(), "api", api)
		return []interface{}{key1, elem1}
	}))
}
