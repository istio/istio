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

package env

import (
	"os"
	"testing"
)

func SkipTSanASan(t *testing.T) {
	if os.Getenv("TSAN") != "" || os.Getenv("ASAN") != "" {
		t.Skip("https://github.com/istio/istio/issues/21273")
	}
}

func SkipTSan(t *testing.T) {
	if os.Getenv("TSAN") != "" {
		t.Skip("https://github.com/istio/istio/issues/21273")
	}
}

func IsTSanASan() bool {
	return os.Getenv("TSAN") != "" || os.Getenv("ASAN") != ""
}
