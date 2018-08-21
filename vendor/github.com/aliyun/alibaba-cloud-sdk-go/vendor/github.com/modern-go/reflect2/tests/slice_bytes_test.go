package tests

import (
	"github.com/modern-go/reflect2"
	"testing"
)

func Test_slice_bytes(t *testing.T) {
	t.Run("SetIndex", testOp(func(api reflect2.API) interface{} {
		obj := [][]byte{[]byte("hello"), []byte("world")}
		valType := api.TypeOf(obj).(reflect2.SliceType)
		elem1 := []byte("hi")
		valType.SetIndex(&obj, 0, &elem1)
		elem2 := []byte("there")
		valType.SetIndex(&obj, 1, &elem2)
		return obj
	}))
}
