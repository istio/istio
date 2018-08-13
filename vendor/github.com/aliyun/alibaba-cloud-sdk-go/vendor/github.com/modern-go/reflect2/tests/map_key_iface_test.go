package tests

import (
	"github.com/modern-go/reflect2"
	"github.com/modern-go/test/must"
	"testing"
)

type intError int

func (err intError) Error() string {
	return ""
}

func Test_map_iface_key(t *testing.T) {
	var pInt = func(val int) *int {
		return &val
	}
	t.Run("SetIndex", testOp(func(api reflect2.API) interface{} {
		obj := map[error]int{}
		valType := api.TypeOf(obj).(reflect2.MapType)
		key1 := error(intError(2))
		valType.SetIndex(&obj, &key1, pInt(4))
		key2 := error(intError(2))
		valType.SetIndex(&obj, &key2, pInt(9))
		key3 := error(nil)
		valType.SetIndex(&obj, &key3, pInt(9))
		must.Panic(func() {
			key4 := ""
			valType.SetIndex(&obj, &key4, pInt(9))
		})
		return obj
	}))
	t.Run("GetIndex", testOp(func(api reflect2.API) interface{} {
		obj := map[error]int{intError(3): 9, intError(2): 4}
		valType := api.TypeOf(obj).(reflect2.MapType)
		must.Panic(func() {
			key1 := ""
			valType.GetIndex(obj, &key1)
		})
		key2 := error(intError(3))
		key3 := error(nil)
		return []interface{}{
			valType.GetIndex(&obj, &key2),
			valType.GetIndex(&obj, &key3),
		}
	}))
	t.Run("Iterate", testOp(func(api reflect2.API) interface{} {
		obj := map[error]int{intError(2): 4}
		valType := api.TypeOf(obj).(reflect2.MapType)
		iter := valType.Iterate(&obj)
		must.Pass(iter.HasNext(), "api", api)
		key1, elem1 := iter.Next()
		must.Pass(!iter.HasNext(), "api", api)
		return []interface{}{key1, elem1}
	}))
}
