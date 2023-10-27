package flag

import (
	"testing"

	"github.com/spf13/pflag"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestBind(t *testing.T) {
	type Options struct {
		A, B, C string
		Env     string
		Bool    bool
	}
	opts := Options{
		B: "b-def",
	}
	test.SetEnvForTest(t, "TEST_ENV", "from-env")
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	Bind(fs, "a", "", "desc", &opts.A)
	Bind(fs, "bool", "", "desc", &opts.Bool)
	BindEnv(fs, "test-env", "", "desc", &opts.Env)
	fs.Set("a", "a-set")
	fs.Set("bool", "true")
	assert.Equal(t, opts, Options{
		A:    "a-set",
		B:    "b-def",
		Env:  "from-env",
		Bool: true,
	})
}
