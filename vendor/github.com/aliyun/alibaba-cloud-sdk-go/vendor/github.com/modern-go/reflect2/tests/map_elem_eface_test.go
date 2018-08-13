package tests

import (
	"github.com/modern-go/reflect2"
	"github.com/modern-go/test/must"
	"testing"

	"context"
	"github.com/modern-go/test"
)

func Test_map_elem_eface(t *testing.T) {
	var pInt = func(val int) *int {
		return &val
	}
	t.Run("SetIndex", testOp(func(api reflect2.API) interface{} {
		obj := map[int]interface{}{}
		valType := api.TypeOf(obj).(reflect2.MapType)
		elem1 := interface{}(4)
		valType.SetIndex(&obj, pInt(2), &elem1)
		elem2 := interface{}(nil)
		valType.SetIndex(&obj, pInt(3), &elem2)
		return obj
	}))
	t.Run("GetIndex", testOp(func(api reflect2.API) interface{} {
		obj := map[int]interface{}{3: 9, 2: nil}
		valType := api.TypeOf(obj).(reflect2.MapType)
		return []interface{}{
			valType.GetIndex(&obj, pInt(3)),
			valType.GetIndex(&obj, pInt(2)),
			valType.GetIndex(&obj, pInt(0)),
		}
	}))
	t.Run("TryGetIndex", test.Case(func(ctx context.Context) {
		obj := map[int]interface{}{3: 9, 2: nil}
		valType := reflect2.TypeOf(obj).(reflect2.MapType)
		elem, found := valType.TryGetIndex(&obj, pInt(3))
		must.Equal(9, *elem.(*interface{}))
		must.Pass(found)
		elem, found = valType.TryGetIndex(&obj, pInt(2))
		must.Nil(*elem.(*interface{}))
		must.Pass(found)
		elem, found = valType.TryGetIndex(&obj, pInt(0))
		must.Nil(elem)
		must.Pass(!found)
	}))
	t.Run("Iterate", testOp(func(api reflect2.API) interface{} {
		obj := map[int]interface{}{2: 4}
		valType := api.TypeOf(obj).(reflect2.MapType)
		iter := valType.Iterate(&obj)
		must.Pass(iter.HasNext(), "api", api)
		key1, elem1 := iter.Next()
		must.Pass(!iter.HasNext(), "api", api)
		return []interface{}{key1, elem1}
	}))
}
