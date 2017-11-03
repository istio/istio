package cmd

import (
	"flag"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"reflect"
)

func TestInitializeFlags(t *testing.T) {
	cmd := &cobra.Command{}
	var testInt int
	flag.IntVar(&testInt, "test", 137, "test int flag")
	InitializeFlags(cmd)

	testName := "Initialize Flags"
	if !flag.Parsed() {
		t.Errorf("%s: flag.Parsed() returns false, should be true", testName)
	}

	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Name != "test" {
			t.Errorf("%s: pflag name error. Actual %s, Expected %s", testName, f.Name, "test")
		}
	})

	_ = cmd.Flags().Parse([]string{})
	if testInt != 137 {
		t.Errorf("%s: pflag parse error. Actual %d, Expected %d", testName, testInt, 137)
	}
}

func TestFilterFlags(t *testing.T) {
	testCases := map[string]struct {
		flags *pflag.FlagSet
		in    []string
		out   []string
	}{
		"empty args": {
			flags: pflag.NewFlagSet("test1", pflag.ContinueOnError),
			in:    []string{},
			out:   []string{},
		},
		"unknown flags": {
			flags: func() *pflag.FlagSet {
				flags := pflag.NewFlagSet("test2", pflag.ContinueOnError)
				_ = flags.String("test", "", "")
				return flags
			}(),
			in:  []string{"--unknown1", "--test", "test arg", "--unknown2", "unknown arg"},
			out: []string{"--test", "test arg"},
		},
		"subcommand with unknown flag": {
			flags: func() *pflag.FlagSet {
				flags := pflag.NewFlagSet("test2", pflag.ContinueOnError)
				_ = flags.String("test", "", "")
				return flags
			}(),
			in:  []string{"subcmd", "--test", "test arg", "--unknown"},
			out: []string{"subcmd", "--test", "test arg"},
		},
		"terminate flags": {
			flags: func() *pflag.FlagSet {
				flags := pflag.NewFlagSet("test2", pflag.ContinueOnError)
				_ = flags.String("test", "", "")
				return flags
			}(),
			in:  []string{"--unknown", "--test", "--", "a", "b", "c"},
			out: []string{"--test", "--", "a", "b", "c"},
		},
	}

	for id, c := range testCases {
		actual := FilterFlags(c.flags, c.in)
		if !reflect.DeepEqual(c.out, actual) {
			t.Errorf("%s: wrong output. Expected %v, Actual %v", id, c.out, actual)
		}
	}
}

func TestAddFlagsAfterParsing(t *testing.T) {
	var conf struct {
		a string
		b string
	}
	testArgs := []string{"--a", "aaa", "--b", "bbb"}

	cmd := &cobra.Command{
		Run: func(cmd *cobra.Command, args []string) {
			if conf.b != "bbb" {
				t.Errorf("Expected: bbb, Actual: %s", conf.a)
			}

			flags := cmd.Flags()
			flags.StringVar(&conf.a, "a", "", "")
			if err := flags.Parse(testArgs); err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if conf.a != "aaa" {
				t.Errorf("Expected: aaa, Actual: %s", conf.a)
			}
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&conf.b, "b", "", "")
	cmd.SetArgs(FilterFlags(flags, testArgs))

	if err := cmd.Execute(); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}
