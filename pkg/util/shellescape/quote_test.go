package shellescape

import (
	"os/exec"
	"strings"
	"testing"
)

func TestQuote(t *testing.T) {
	testCases := []string{
		`{"key": "value"}`,
		`it's going to need single quotes`,
		"",
		"no issues here",
	}
	for _, original := range testCases {
		out, err := exec.Command("sh", "-c", "echo "+Quote(original)).CombinedOutput()
		if err != nil {
			t.Fatal(err)
		}
		got := strings.TrimRight(string(out), "\n")
		if got != original {
			t.Errorf("got: %s; want: %s", got, original)
		}
	}
}
