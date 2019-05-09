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

package file

import (
	"io/ioutil"
	"testing"
)

// AsBytes is a utility function that reads the content of the given file
// and fails the test if an error occurs.
func AsBytes(t testing.TB, filename string) []byte {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

// AsString calls AsBytes and then converts to string.
func AsString(t testing.TB, filename string) string {
	return string(AsBytes(t, filename))
}
