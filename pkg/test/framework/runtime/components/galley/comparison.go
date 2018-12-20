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

	"github.com/hashicorp/go-multierror"
	"github.com/pmezard/go-difflib/difflib"
)

type comparisonResult struct {
	actual          map[string]interface{}
	expected        map[string]interface{}
	extraActual     []string
	missingExpected []string
	conflicting     []string
}

func (r *comparisonResult) generateError() (err error) {

	for _, n := range r.missingExpected {
		js, er := json.MarshalIndent(r.expected[n], "", "  ")
		if er != nil {
			return er
		}

		err = multierror.Append(err, fmt.Errorf("expected resource not found: %s\n%v\n", n, string(js)))
	}

	for _, n := range r.extraActual {
		js, er := json.MarshalIndent(r.expected[n], "", "  ")
		if er != nil {
			return er
		}

		err = multierror.Append(err, fmt.Errorf("extra resource not found: %s\n%v\n", n, string(js)))
	}

	for _, n := range r.conflicting {
		ajs, er := json.MarshalIndent(r.actual[n], "", "  ")
		if er != nil {
			return er
		}
		ejs, er := json.MarshalIndent(r.expected[n], "", "  ")
		if er != nil {
			return er
		}

		diff := difflib.UnifiedDiff{
			FromFile: fmt.Sprintf("Expected %q", n),
			A:        difflib.SplitLines(string(ejs)),
			ToFile:   fmt.Sprintf("Actual %q", n),
			B:        difflib.SplitLines(string(ajs)),
			Context:  10,
		}
		text, er := difflib.GetUnifiedDiffString(diff)
		if er != nil {
			return er
		}

		err = multierror.Append(err, fmt.Errorf("resource mismatch: %q\n%s\n",
			n, text))
	}

	return multierror.Append(err).ErrorOrNil()
}
