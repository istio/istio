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

// This file has a series of fuzzers that target different
// parts of Istio. They are placed here because it does not
// make sense to place them in different files yet.
// The fuzzers can be moved to other files without anything
// breaking on the OSS-fuzz side.

package fuzz

import (
	fuzz "github.com/AdaLogics/go-fuzz-headers"

	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/resource"
)

func FuzzGalleyDiag(data []byte) int {
	f := fuzz.NewConsumer(data)

	code, err := f.GetString()
	if err != nil {
		return 0
	}
	templ, err := f.GetString()
	if err != nil {
		return 0
	}
	mt := diag.NewMessageType(diag.Error, code, templ)
	resourceIsNil, err := f.GetBool()
	if err != nil {
		return 0
	}
	parameter, err := f.GetString()
	if err != nil {
		return 0
	}
	var ri *resource.Instance
	if resourceIsNil {
		ri = nil
	} else {
		err = f.GenerateStruct(ri)
		if err != nil {
			return 0
		}
	}
	m := diag.NewMessage(mt, ri, parameter)
	_ = m.Unstructured(true)
	_ = m.UnstructuredAnalysisMessageBase()
	_ = m.Origin()
	_, _ = m.MarshalJSON()
	_ = m.String()
	replStr, err := f.GetString()
	if err == nil {
		_ = m.ReplaceLine(replStr)
	}
	return 1
}
