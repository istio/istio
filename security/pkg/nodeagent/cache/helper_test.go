// Copyright 2019 Istio Authors
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

package cache

import (
	"io/ioutil"
	"testing"
)

func TestIsTrustworthyJwt(t *testing.T) {
	testCases := []struct {
		Name           string
		Jwt            string
		ExpectedResult bool
	}{
		{
			Name:           "legacy jwt",
			Jwt:            getJwtFromFile("testdata/legacy-jwt.jwt", t),
			ExpectedResult: false,
		},
		{
			Name:           "trustworthy jwt",
			Jwt:            getJwtFromFile("testdata/trustworthy-jwt.jwt", t),
			ExpectedResult: true,
		},
	}

	for _, tc := range testCases {
		isTrustworthyJwt, err := isTrustworthyJwt(tc.Jwt)
		if err != nil {
			t.Errorf("%s failed with error %v", tc.Name, err.Error())
		}
		if isTrustworthyJwt != tc.ExpectedResult {
			t.Errorf("%s failed. For ExpectedResult: want result %v, got %v\n", tc.Name, tc.ExpectedResult, isTrustworthyJwt)
		}
	}
}

func getJwtFromFile(filePath string, t *testing.T) string {
	jwt, err := ioutil.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read %q", filePath)
	}
	return string(jwt)
}
