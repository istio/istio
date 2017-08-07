// This file provides testing of the bazel rules in generate.bzl. It is meant
// to test the larger bazel function of invoking model generation correctly.

package pkg

import (
	"bytes"
	"io"
	"os"
	"testing"
)

type logFn func(string, ...interface{})

// TestBazelGeneration uses the outputs files generated via the bazel rule
// for testdata:generated_files and compares them against the golden files.
func TestBazelGeneration(t *testing.T) {
	tests := []struct {
		name, got, want string
	}{
		{"Metrics", "interfacegen/testdata/metric_template_library_handler.gen.go", "interfacegen/testdata/MetricTemplateHandlerInterface.golden.go"},
		{"Quota", "interfacegen/testdata/quota_template_library_handler.gen.go", "interfacegen/testdata/QuotaTemplateHandlerInterface.golden.go"},
		{"Logs", "interfacegen/testdata/log_template_library_handler.gen.go", "interfacegen/testdata/LogTemplateHandlerInterface.golden.go"},
		{"Lists", "interfacegen/testdata/list_template_library_handler.gen.go", "interfacegen/testdata/ListTemplateHandlerInterface.golden.go"},
	}
	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			if same := fileCompare(v.got, v.want, t.Errorf); !same {
				t.Error("Files were not the same.")
			}
		})
	}
}

const chunkSize = 64000

func fileCompare(file1, file2 string, logf logFn) bool {
	f1, err := os.Open(file1)
	if err != nil {
		logf("could not open file: %v", err)
		return false
	}

	f2, err := os.Open(file2)
	if err != nil {
		logf("could not open file: %v", err)
		return false
	}

	for {
		b1 := make([]byte, chunkSize)
		s1, err1 := f1.Read(b1)

		b2 := make([]byte, chunkSize)
		s2, err2 := f2.Read(b2)

		if err1 == io.EOF && err2 == io.EOF {
			return true
		}

		if err1 != nil || err2 != nil {
			return false
		}

		if !bytes.Equal(b1, b2) {
			logf("bytes don't match (sizes: %d, %d):\n%s\n%s", s1, s2, string(b1), string(b2))
			return false
		}
	}
}
