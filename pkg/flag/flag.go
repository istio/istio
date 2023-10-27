package flag

import (
	"os"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

// Flaggable defines the set of types that can be flags.
// This is not exhaustive; add more as needed
type Flaggable interface {
	string | bool | uint16 | time.Duration
}

var replacer = strings.NewReplacer("-", "_")

// Bind registers a flag to the FlagSet. When parsed, the value will be set via pointer.
// Usage:
//
//	cfg := Config{Foo: "default-foo"}
//	flag.Bind(fs, "foo", "f", "the foo value", &cfg.Foo)
func Bind[T Flaggable](fs *pflag.FlagSet, name, shorthand, usage string, val *T) {
	switch d := any(val).(type) {
	case *string:
		fs.StringVarP(d, name, shorthand, *d, usage)
	case *bool:
		fs.BoolVarP(d, name, shorthand, *d, usage)
	case *time.Duration:
		fs.DurationVarP(d, name, shorthand, *d, usage)
	case *uint16:
		fs.Uint16VarP(d, name, shorthand, *d, usage)
	}
}

// BindEnv behaves like Bind, but additionally allows an environment variable to override.
// This will transform to name field: foo-bar becomes FOO_BAR.
func BindEnv[T Flaggable](fs *pflag.FlagSet, name, shorthand, usage string, val *T) {
	Bind(fs, name, shorthand, usage, val)
	en := strings.ToUpper(replacer.Replace(name))
	if v, f := os.LookupEnv(en); f {
		_ = fs.Set(name, v)
	}
}

// AdditionalEnv allows additional env vars to set the flag value as well.
// Unlike BindEnv, this does not do any transformations.
func AdditionalEnv(fs *pflag.FlagSet, flagName, envName string) {
	if v, f := os.LookupEnv(envName); f {
		_ = fs.Set(flagName, v)
	}
}
