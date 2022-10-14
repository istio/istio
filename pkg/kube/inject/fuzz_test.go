// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package inject

import (
	"testing"

	fuzz "github.com/AdaLogics/go-fuzz-headers"
)

func FuzzRunTemplate(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte, v string) {
		ff := fuzz.NewConsumer(data)

		// create injection parameters
		IP := InjectionParameters{}
		err := ff.GenerateStruct(&IP)
		if err != nil {
			return
		}
		if IP.pod == nil {
			return
		}
		vc, err := NewValuesConfig(v)
		if err != nil {
			return
		}
		IP.valuesConfig = vc

		// call RunTemplate()
		_, _, _ = RunTemplate(IP)
	})
}
