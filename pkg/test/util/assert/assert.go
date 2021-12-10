package assert

import (
	"strings"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"istio.io/istio/pkg/test"
)

// Equal
func Equal(t test.Failer, a, b interface{}, context ...string) {
	t.Helper()
	if !cmp.Equal(a, b, protocmp.Transform()) {
		cs := ""
		if len(context) > 0 {
			cs = " " + strings.Join(context, ", ") + ":"
		}
		t.Fatal("found diff:%s %v", cs, cmp.Diff(a, b, protocmp.Transform()))
	}
}

func Error(t test.Failer, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}

func NoError(t test.Failer, err error) {
	t.Helper()
	if err != nil {
		t.Fatal("expected no error but got: %v", err)
	}
}
