package keepalive_test

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"istio.io/istio/pkg/keepalive"
)

// Test default maximum connection age is set to infinite, preserving previous
// unbounded lifetime behavior.
func TestAgeDefaultsToInfinite(t *testing.T) {
	ko := keepalive.DefaultOption()

	if ko.MaxServerConnectionAge != keepalive.Infinity {
		t.Errorf("%s maximum connection age %v", t.Name(), ko.MaxServerConnectionAge)
	}
}

// Confirm maximum connection age parameters can be set from the command line.
func TestSetConnectionAgeCommandlineOptions(t *testing.T) {
	ko := keepalive.DefaultOption()
	cmd := &cobra.Command{}
	ko.AttachCobraFlags(cmd)

	buf := new(bytes.Buffer)
	cmd.SetOutput(buf)
	sec := 1 * time.Second
	cmd.SetArgs([]string{
		fmt.Sprintf("--keepaliveMaxServerConnectionAge=%v", sec),
	})

	if err := cmd.Execute(); err != nil {
		t.Errorf("%s %s", t.Name(), err.Error())
	}
	if ko.MaxServerConnectionAge != sec {
		t.Errorf("%s maximum connection age %v", t.Name(), ko.MaxServerConnectionAge)
	}
}
