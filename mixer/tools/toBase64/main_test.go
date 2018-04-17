// Copyright 2017 Istio Authors
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

//go:generate protoc testdata/foo.proto -otestdata/foo.descriptor -I$GOPATH/src/istio.io/istio/vendor/istio.io/api -I.
package main

import (
	"io/ioutil"
	//"os"
	"fmt"
	"os"
	"strings"
	"testing"
)

type testdata struct {
	in          string
	expected    string
	expectedErr string
}

func TestToBase64(t *testing.T) {
	file, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("tmp baseline ", file.Name())
	defer func() {
		if removeErr := os.Remove(file.Name()); removeErr != nil {
			t.Logf("could not remove temporary file %s: %v", file.Name(), removeErr)
		}
	}()

	for i, td := range []testdata{
		{
			in:       "testdata/foo.descriptor",
			expected: "testdata/foo.baseline",
		},
		{
			in:          "invalid-file-path",
			expectedErr: "invalid path",
		},
	} {
		t.Run(fmt.Sprintf("%d", i), func(tt *testing.T) {
			var args []string
			if td.in != "" {
				args = []string{td.in, "-o", file.Name()}
			} else {
				args = []string{"", "-o", file.Name()}
			}

			gotError := ""
			withArgs(args,
				func(format string, a ...interface{}) {
					gotError = fmt.Sprintf(format, a...)
					if td.expectedErr == "" {
						tt.Fatalf("want error 'nil'; got '%s'", gotError)
					}
					if !strings.Contains(gotError, td.expectedErr) {
						tt.Fatalf("want error '%s'; got '%s'", td.expectedErr, gotError)
					}
				})

			if gotError == "" {
				if td.expectedErr != "" {
					tt.Fatalf("want error '%s'; got 'nil'", td.expectedErr)
				}

				// compare the result
				wantbyts, _ := ioutil.ReadFile(td.expected)
				gotbyts, _ := ioutil.ReadFile(file.Name())
				wantStr := string(wantbyts)
				gotStr := string(gotbyts)

				if gotStr != wantStr {
					tt.Errorf("want '%s'; got '%s'", wantStr, gotStr)
				}
			}
		})
	}
}

func TestToBase64_NoInputFile(t *testing.T) {
	var gotError string
	withArgs([]string{},
		func(format string, a ...interface{}) {
			gotError = fmt.Sprintf(format, a...)
			if !strings.Contains(gotError, "must specify an input file") {
				t.Fatalf("want error 'must specify an input file'; got '%s'", gotError)
			}
		})

	if gotError == "" {
		t.Errorf("want error; got nil")
	}
}

func TestToBase64_NoOutputFile(t *testing.T) {
	withArgs([]string{"testdata/foo.descriptor"},
		func(format string, a ...interface{}) {
			t.Fatalf("want no error; got '%s'", fmt.Sprintf(format, a...))
		})
}

func TestToBase64_BadOutputFile(t *testing.T) {
	var gotError string
	withArgs([]string{"testdata/foo.descriptor", "-o", "bad/directory/bad file path"},
		func(format string, a ...interface{}) {
			gotError = fmt.Sprintf(format, a...)
			if !strings.Contains(gotError, "cannot write to output file") {
				t.Fatalf("want error 'cannot write to output file'; got '%s'", gotError)
			}
		})
	if gotError == "" {
		t.Errorf("want error; got nil")
	}
}
