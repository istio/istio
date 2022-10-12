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

package ra

import (
	"testing"

	fuzz "github.com/AdaLogics/go-fuzz-headers"
)

func FuzzValidateCSR(f *testing.F) {
	f.Fuzz(func(t *testing.T, csrPEM, subjectIDsData []byte) {
		ff := fuzz.NewConsumer(subjectIDsData)

		// create subjectIDs
		subjectIDs := make([]string, 0)
		noOfEntries, err := ff.GetUint64()
		if err != nil {
			return
		}
		var i uint64
		for i = 0; i < noOfEntries; i++ {
			newStr, err := ff.GetString()
			if err != nil {
				break
			}
			subjectIDs = append(subjectIDs, newStr)
		}

		// call ValidateCSR()
		ValidateCSR(csrPEM, subjectIDs)
	})
}
