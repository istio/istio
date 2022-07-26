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

// nolint: revive
package fuzz

import (
	"bytes"
	"errors"

	fuzz "github.com/AdaLogics/go-fuzz-headers"

	"istio.io/istio/istioctl/pkg/writer/compare"
)

func createIstiodDumps(f *fuzz.ConsumeFuzzer) (map[string][]byte, error) {
	m := make(map[string][]byte)
	maxNoEntries := 50
	qty, err := f.GetInt()
	if err != nil {
		return m, err
	}
	noOfEntries := qty % maxNoEntries
	if noOfEntries == 0 {
		return m, errors.New("a map of zero-length was created")
	}
	for i := 0; i < noOfEntries; i++ {
		k, err := f.GetString()
		if err != nil {
			return m, err
		}
		v, err := f.GetBytes()
		if err != nil {
			return m, err
		}
		m[k] = v
	}
	return m, nil
}

func FuzzCompareDiff(data []byte) int {
	f := fuzz.NewConsumer(data)
	istiodDumps, err := createIstiodDumps(f)
	if err != nil {
		return 0
	}
	envoyDump, err := f.GetBytes()
	if err != nil {
		return 0
	}
	var buf bytes.Buffer
	c, err := compare.NewComparator(&buf, istiodDumps, envoyDump)
	if err != nil {
		return 0
	}
	_ = c.Diff()
	return 1
}
