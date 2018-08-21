package test

import (
	"github.com/modern-go/reflect2"
	"testing"
)

func Test_map(t *testing.T) {
	var pInt = func(val int) *int {
		return &val
	}
	valType := reflect2.TypeOf(map[int]int{}).(reflect2.MapType)
	m := map[int]int{}
	valType.SetIndex(&m, pInt(1), pInt(1))
	if m[1] != 1 {
		t.Fail()
	}
}
