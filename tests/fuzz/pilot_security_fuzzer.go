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

package fuzz

import (
	"fmt"

	fuzz "github.com/AdaLogics/go-fuzz-headers"

	"istio.io/istio/pilot/pkg/security/authz/matcher"
)

func FuzzCidrRange(data []byte) int {
	_, _ = matcher.CidrRange(string(data))
	return 1
}

func FuzzHeaderMatcher(data []byte) int {
	k, v, err := getKandV(data)
	if err != nil {
		return 0
	}
	_ = matcher.HeaderMatcher(k, v)
	return 1
}

func FuzzHostMatcherWithRegex(data []byte) int {
	k, v, err := getKandV(data)
	if err != nil {
		return 0
	}
	_ = matcher.HostMatcherWithRegex(k, v)
	return 1
}

func FuzzHostMatcher(data []byte) int {
	k, v, err := getKandV(data)
	if err != nil {
		return 0
	}
	_ = matcher.HostMatcher(k, v)
	return 1
}

func FuzzMetadataListMatcher(data []byte) int {
	f := fuzz.NewConsumer(data)
	filter, err := f.GetString()
	if err != nil {
		return 0
	}
	number, err := f.GetInt()
	if err != nil {
		return 0
	}
	maxKeys := number % 100
	keys := make([]string, maxKeys)
	for i := 0; i < maxKeys; i++ {
		key, err := f.GetString()
		if err != nil {
			return 0
		}
		keys = append(keys, key)
	}
	value, err := f.GetString()
	if err != nil {
		return 0
	}
	_ = matcher.MetadataListMatcher(filter, keys, matcher.StringMatcher(value))
	return 1
}

func getKandV(data []byte) (string, string, error) {
	if len(data) < 10 {
		return "", "", fmt.Errorf("not enough bytes")
	}
	if len(data)%2 != 0 {
		return "", "", fmt.Errorf("not correct amount of bytes")
	}
	k := string(data[:len(data)/2])
	v := string(data[(len(data)/2)+1:])
	return k, v, nil
}
