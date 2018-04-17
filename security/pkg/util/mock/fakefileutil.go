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

package mock

import (
	"fmt"
	"os"
)

// FakeFileUtil is a mocked FileUtil for testing.
type FakeFileUtil struct {
	ReadContent  map[string][]byte
	WriteContent map[string][]byte
}

// Read returns the filename entry in ReadContent or an error.
func (f FakeFileUtil) Read(filename string) ([]byte, error) {
	if f.ReadContent[filename] != nil {
		return f.ReadContent[filename], nil
	}
	return nil, fmt.Errorf("file not found")
}

// Write writes data to the filename entry in WriteContent.
func (f FakeFileUtil) Write(filename string, content []byte, perm os.FileMode) error {
	if f.WriteContent == nil {
		f.WriteContent = make(map[string][]byte)
	}
	f.WriteContent[filename] = content
	return nil
}
