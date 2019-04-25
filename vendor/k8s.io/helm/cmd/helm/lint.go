/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/lint"
	"k8s.io/helm/pkg/lint/support"
	"k8s.io/helm/pkg/strvals"
)

var longLintHelp = `
This command takes a path to a chart and runs a series of tests to verify that
the chart is well-formed.

If the linter encounters things that will cause the chart to fail installation,
it will emit [ERROR] messages. If it encounters issues that break with convention
or recommendation, it will emit [WARNING] messages.
`

type lintCmd struct {
	valueFiles valueFiles
	values     []string
	sValues    []string
	fValues    []string
	namespace  string
	strict     bool
	paths      []string
	out        io.Writer
}

func newLintCmd(out io.Writer) *cobra.Command {
	l := &lintCmd{
		paths: []string{"."},
		out:   out,
	}
	cmd := &cobra.Command{
		Use:   "lint [flags] PATH",
		Short: "examines a chart for possible issues",
		Long:  longLintHelp,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				l.paths = args
			}
			return l.run()
		},
	}

	cmd.Flags().VarP(&l.valueFiles, "values", "f", "specify values in a YAML file (can specify multiple)")
	cmd.Flags().StringArrayVar(&l.values, "set", []string{}, "set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	cmd.Flags().StringArrayVar(&l.sValues, "set-string", []string{}, "set STRING values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	cmd.Flags().StringArrayVar(&l.fValues, "set-file", []string{}, "set values from respective files specified via the command line (can specify multiple or separate values with commas: key1=path1,key2=path2)")
	cmd.Flags().StringVar(&l.namespace, "namespace", "default", "namespace to put the release into")
	cmd.Flags().BoolVar(&l.strict, "strict", false, "fail on lint warnings")

	return cmd
}

var errLintNoChart = errors.New("No chart found for linting (missing Chart.yaml)")

func (l *lintCmd) run() error {
	var lowestTolerance int
	if l.strict {
		lowestTolerance = support.WarningSev
	} else {
		lowestTolerance = support.ErrorSev
	}

	// Get the raw values
	rvals, err := l.vals()
	if err != nil {
		return err
	}

	var total int
	var failures int
	for _, path := range l.paths {
		if linter, err := lintChart(path, rvals, l.namespace, l.strict); err != nil {
			fmt.Println("==> Skipping", path)
			fmt.Println(err)
			if err == errLintNoChart {
				failures = failures + 1
			}
		} else {
			fmt.Println("==> Linting", path)

			if len(linter.Messages) == 0 {
				fmt.Println("Lint OK")
			}

			for _, msg := range linter.Messages {
				fmt.Println(msg)
			}

			total = total + 1
			if linter.HighestSeverity >= lowestTolerance {
				failures = failures + 1
			}
		}
		fmt.Println("")
	}

	msg := fmt.Sprintf("%d chart(s) linted", total)
	if failures > 0 {
		return fmt.Errorf("%s, %d chart(s) failed", msg, failures)
	}

	fmt.Fprintf(l.out, "%s, no failures\n", msg)

	return nil
}

func lintChart(path string, vals []byte, namespace string, strict bool) (support.Linter, error) {
	var chartPath string
	linter := support.Linter{}

	if strings.HasSuffix(path, ".tgz") {
		tempDir, err := ioutil.TempDir("", "helm-lint")
		if err != nil {
			return linter, err
		}
		defer os.RemoveAll(tempDir)

		file, err := os.Open(path)
		if err != nil {
			return linter, err
		}
		defer file.Close()

		if err = chartutil.Expand(tempDir, file); err != nil {
			return linter, err
		}

		lastHyphenIndex := strings.LastIndex(filepath.Base(path), "-")
		if lastHyphenIndex <= 0 {
			return linter, fmt.Errorf("unable to parse chart archive %q, missing '-'", filepath.Base(path))
		}
		base := filepath.Base(path)[:lastHyphenIndex]
		chartPath = filepath.Join(tempDir, base)
	} else {
		chartPath = path
	}

	// Guard: Error out of this is not a chart.
	if _, err := os.Stat(filepath.Join(chartPath, "Chart.yaml")); err != nil {
		return linter, errLintNoChart
	}

	return lint.All(chartPath, vals, namespace, strict), nil
}

// vals merges values from files specified via -f/--values and
// directly via --set or --set-string or --set-file, marshaling them to YAML
//
// This func is implemented intentionally and separately from the `vals` func for the `install` and `upgrade` comammdsn.
// Compared to the alternative func, this func lacks the parameters for tls opts - ca key, cert, and ca cert.
// That's because this command, `lint`, is explicitly forbidden from making server connections.
func (l *lintCmd) vals() ([]byte, error) {
	base := map[string]interface{}{}

	// User specified a values files via -f/--values
	for _, filePath := range l.valueFiles {
		currentMap := map[string]interface{}{}
		bytes, err := ioutil.ReadFile(filePath)
		if err != nil {
			return []byte{}, err
		}

		if err := yaml.Unmarshal(bytes, &currentMap); err != nil {
			return []byte{}, fmt.Errorf("failed to parse %s: %s", filePath, err)
		}
		// Merge with the previous map
		base = mergeValues(base, currentMap)
	}

	// User specified a value via --set
	for _, value := range l.values {
		if err := strvals.ParseInto(value, base); err != nil {
			return []byte{}, fmt.Errorf("failed parsing --set data: %s", err)
		}
	}

	// User specified a value via --set-string
	for _, value := range l.sValues {
		if err := strvals.ParseIntoString(value, base); err != nil {
			return []byte{}, fmt.Errorf("failed parsing --set-string data: %s", err)
		}
	}

	// User specified a value via --set-file
	for _, value := range l.fValues {
		reader := func(rs []rune) (interface{}, error) {
			bytes, err := ioutil.ReadFile(string(rs))
			return string(bytes), err
		}
		if err := strvals.ParseIntoFile(value, base, reader); err != nil {
			return []byte{}, fmt.Errorf("failed parsing --set-file data: %s", err)
		}
	}

	return yaml.Marshal(base)
}
