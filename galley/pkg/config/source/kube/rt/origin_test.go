package rt

import (
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
)

func TestPositionString(t *testing.T) {
	testcases := []struct {
		filename string
		line     int
		output   string
	}{
		{
			filename: "test.yaml",
			line:     1,
			output:   "test.yaml:1",
		},
		{
			filename: "test.yaml",
			line:     0,
			output:   "test.yaml",
		},
		{
			filename: "test.json",
			line:     1,
			output:   "test.json",
		},
		{
			filename: "-",
			line:     1,
			output:   "-:1",
		},
		{
			filename: "",
			line:     1,
			output:   "",
		},
	}
	for i, tc := range testcases {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			g := NewGomegaWithT(t)

			p := Position{Filename: tc.filename, Line: tc.line}
			g.Expect(p.String()).To(Equal(tc.output))
		})
	}
}
