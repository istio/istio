package tests

import (
	"github.com/modern-go/reflect2"
	"testing"
)

func Test_slice_string(t *testing.T) {
	t.Run("SetIndex", testOp(func(api reflect2.API) interface{} {
		obj := []string{"hello", "world"}
		valType := api.TypeOf(obj).(reflect2.SliceType)
		elem1 := "hi"
		valType.SetIndex(&obj, 0, &elem1)
		elem2 := "there"
		valType.SetIndex(&obj, 1, &elem2)
		return obj
	}))
}
