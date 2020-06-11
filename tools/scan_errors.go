// Copyright 2020 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

/*
The program scan_errors.go recursively scans a directory path and, for each file, counts the number of log errors and
warnings contained in it. It prints out a summary of counts, aggregated by all subpaths.
*/

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"istio.io/istio/operator/pkg/util"
)

//nolint: gocognit
func main() {
	var rootDir string
	var includeFmt bool

	flag.StringVar(&rootDir, "d", ".", "Path to root dir. Current working dir is used if unset.")
	flag.BoolVar(&includeFmt, "f", false, "Include fmt.Errorf in counts.")
	flag.Parse()

	files, err := util.FindFiles(rootDir, goFileFilter)
	if err != nil {
		fmt.Println(err)
		os.Exit(1) //nolint: gomnd
	}

	totErr, totWarn := 0, 0
	fileErr, fileWarn := make(map[string]int), make(map[string]int)

	for _, file := range files {
		fmt.Printf("Processing %s.\n", file)
		lines, err := getFileLines(file)
		if err != nil {
			fmt.Println(err)
			os.Exit(1) //nolint: gomnd
		}

		for i := 0; i < len(lines); {
			l := lines[i]
			if hasErrorStatement(l, includeFmt) {
				totErr++
				incAllSubpaths(fileErr, file)
			}
			if hasWarningStatement(l) {
				totWarn++
				incAllSubpaths(fileWarn, file)
			}
			i++
		}
	}

	var paths []string
	for file := range fileErr {
		paths = append(paths, file)

	}
	sort.Sort(byLength(paths))
	for _, path := range paths {
		fmt.Printf("%s: %d/%d\n", path, fileErr[path], fileWarn[path])
	}

	fmt.Printf("Total errors = %d, total warnings = %d\n\n", totErr, totWarn)
}

// getFileLines reads the text file at filePath and returns it as a slice of strings.
func getFileLines(filePath string) ([]string, error) {
	b, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	return strings.Split(string(b), "\n"), nil
}

// goFileFilter returns true if path points to a non-test .go file.
func goFileFilter(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, ".go") && !strings.HasSuffix(base, "_test.go")
}

// hasErrorStatement reports whether s contains an error logging statement. includeFmt selects whether fmt.Error
// statements are included.
func hasErrorStatement(s string, includeFmt bool) bool {
	ret := strings.Contains(s, "log.Error") || strings.Contains(s, "scope.Error")
	if includeFmt {
		ret = ret || strings.Contains(s, "fmt.Error")
	}
	return ret
}

// hasWarningStatement reports whether s contains a warning logging statement.
func hasWarningStatement(s string) bool {
	return strings.Contains(s, ".Warn") || strings.Contains(s, "scope.Warn")
}

// incAllSubpaths increments entries in the map for every subpath string of path in m.
func incAllSubpaths(m map[string]int, path string) {
	var cp []string
	for _, pe := range strings.Split(filepath.Clean(path), "/") {
		cp = append(cp, pe)
		soFar := filepath.Join(cp...)
		if soFar == "" {
			soFar = "."
		}
		m[soFar] = m[soFar] + 1
	}
}

// byLength is used to sort by path length and then alphabetically.
type byLength []string

func (s byLength) Len() int {
	return len(s)
}
func (s byLength) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s byLength) Less(i, j int) bool {
	s1 := strings.Split(s[i], "/")
	s2 := strings.Split(s[j], "/")
	if len(s1) == len(s2) {
		return s[i] < s[j]
	}
	return len(s1) > len(s2)
}
