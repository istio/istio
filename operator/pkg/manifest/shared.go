// Copyright Istio Authors
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

package manifest

import (
	"fmt"
	"io"
	"os"
	"strings"

	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/util"
)

func ReadLayeredYAMLs(filenames []string) (string, error) {
	return readLayeredYAMLs(filenames, os.Stdin)
}

func readLayeredYAMLs(filenames []string, stdinReader io.Reader) (string, error) {
	var ly string
	var stdin bool
	for _, fn := range filenames {
		var b []byte
		var err error
		if fn == "-" {
			if stdin {
				continue
			}
			stdin = true
			b, err = io.ReadAll(stdinReader)
		} else {
			b, err = os.ReadFile(strings.TrimSpace(fn))
		}
		if err != nil {
			return "", err
		}
		multiple := false
		multiple, err = hasMultipleIOPs(string(b))
		if err != nil {
			return "", err
		}
		if multiple {
			return "", fmt.Errorf("input file %s contains multiple IstioOperator CRs, only one per file is supported", fn)
		}
		ly, err = util.OverlayIOP(ly, string(b))
		if err != nil {
			return "", err
		}
	}
	return ly, nil
}

func hasMultipleIOPs(s string) (bool, error) {
	objs, err := object.ParseK8sObjectsFromYAMLManifest(s)
	if err != nil {
		return false, err
	}
	found := false
	for _, o := range objs {
		if o.Kind == name.IstioOperator {
			if found {
				return true, nil
			}
			found = true
		}
	}
	return false, nil
}

// GetValueForSetFlag parses the passed set flags which have format key=value and if any set the given path,
// returns the corresponding value, otherwise returns the empty string. setFlags must have valid format.
func GetValueForSetFlag(setFlags []string, path string) string {
	ret := ""
	for _, sf := range setFlags {
		p, v := getPV(sf)
		if p == path {
			ret = v
		}
		// if set multiple times, return last set value
	}
	return ret
}

// getPV returns the path and value components for the given set flag string, which must be in path=value format.
func getPV(setFlag string) (path string, value string) {
	pv := strings.Split(setFlag, "=")
	if len(pv) != 2 {
		return setFlag, ""
	}
	path, value = strings.TrimSpace(pv[0]), strings.TrimSpace(pv[1])
	return
}
