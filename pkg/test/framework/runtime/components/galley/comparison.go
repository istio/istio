//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package galley

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"

	"github.com/hashicorp/go-multierror"
)

type comparisonResult struct {
	actualMap       map[string]interface{}
	expectedMap     map[string]interface{}
	extraActual     []string
	missingExpected []string
	conflicts       []string
}

func (r *comparisonResult) generateError() (err error) {

	for _, n := range r.missingExpected {
		js, er := json.MarshalIndent(r.expectedMap[n], "", "  ")
		if er != nil {
			return er
		}

		err = multierror.Append(err, fmt.Errorf("expected resource not found: %s\n%v\n", n, string(js)))
	}

	for _, n := range r.extraActual {
		js, er := json.MarshalIndent(r.expectedMap[n], "", "  ")
		if er != nil {
			return er
		}

		err = multierror.Append(err, fmt.Errorf("extra resource not found: %s\n%v\n", n, string(js)))
	}

	for _, n := range r.conflicts {
		ajs, er := json.MarshalIndent(r.actualMap[n], "", "  ")
		if er != nil {
			return er
		}
		ejs, er := json.MarshalIndent(r.expectedMap[n], "", "  ")
		if er != nil {
			return er
		}

		err = multierror.Append(err, fmt.Errorf("resource mismatch: %s\ngot:\n%v\nwanted:\n%v\n",
			n, string(ajs), string(ejs)))
	}

	return multierror.Append(err).ErrorOrNil()
}

func (r *comparisonResult) generateDiffFolders(dir string) (string, string, error) {
	actualPath := path.Join(dir, "actual")
	if err := os.Mkdir(actualPath, os.ModePerm); err != nil {
		return "", "", err
	}

	expectedPath := path.Join(dir, "expected")
	if err := os.Mkdir(expectedPath, os.ModePerm); err != nil {
		return "", "", err
	}

	for n, a := range r.actualMap {
		ajs, err := json.MarshalIndent(a, "", "  ")
		if err != nil {
			return "", "", err
		}

		if err = ioutil.WriteFile(path.Join(actualPath, strings.Replace(n, "/", "_", -1)+".json"), ajs, os.ModePerm); err != nil {
			return "", "", err
		}
	}

	for n, a := range r.expectedMap {
		ajs, err := json.MarshalIndent(a, "", "  ")
		if err != nil {
			return "", "", err
		}

		if err = ioutil.WriteFile(path.Join(expectedPath, strings.Replace(n, "/", "_", -1)+".json"), ajs, os.ModePerm); err != nil {
			return "", "", err
		}
	}

	return actualPath, expectedPath, nil
}
