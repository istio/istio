package tests

import (
	"testing"

	"context"
	"github.com/modern-go/reflect2"
	"github.com/modern-go/test"
	"github.com/modern-go/test/must"
)

func Test_struct_ptr(t *testing.T) {
	type TestObject struct {
		Field1 *int
	}
	t.Run("PackEFace", test.Case(func(ctx context.Context) {
		valType := reflect2.TypeOf(TestObject{})
		ptr := valType.UnsafeNew()
		must.Equal(&TestObject{}, valType.PackEFace(ptr))
	}))
	t.Run("Indirect", test.Case(func(ctx context.Context) {
		valType := reflect2.TypeOf(TestObject{})
		must.Equal(TestObject{}, valType.Indirect(&TestObject{}))
	}))
}
