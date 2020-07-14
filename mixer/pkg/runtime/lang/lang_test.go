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

package lang_test

import (
	"testing"

	"istio.io/api/annotation"
	"istio.io/istio/mixer/pkg/runtime/lang"
)

func TestRuntimes(t *testing.T) {
	if got := lang.GetLanguageRuntime(map[string]string{annotation.PolicyLang.Name: lang.CEL.String()}); got != lang.CEL {
		t.Errorf("GetLanguageRuntime => got %s, want CEL", got)
	}
	if got := lang.GetLanguageRuntime(nil); got != lang.CEXL {
		t.Errorf("GetLanguageRuntime => got %s, want CEXL", got)
	}
	if got := lang.GetLanguageRuntime(map[string]string{annotation.PolicyLang.Name: lang.CEXL.String()}); got != lang.CEXL {
		t.Errorf("GetLanguageRuntime => got %s, want CEXL", got)
	}
	if got := lang.GetLanguageRuntime(map[string]string{annotation.PolicyLang.Name: lang.COMPAT.String()}); got != lang.COMPAT {
		t.Errorf("GetLanguageRuntime => got %s, want COMPAT", got)
	}
}
