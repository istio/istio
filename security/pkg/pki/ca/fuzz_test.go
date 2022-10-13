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

package ca

import (
	"testing"
	"time"

	fuzz "github.com/AdaLogics/go-fuzz-headers"
)

func FuzzIstioCASign(f *testing.F) {
	f.Fuzz(func(t *testing.T, data, csrPEM []byte) {
		ff := fuzz.NewConsumer(data)
		// create ca options
		opts := &IstioCAOptions{}
		err := ff.GenerateStruct(opts)
		if err != nil {
			return
		}
		ca, err := NewIstioCA(opts)
		if err != nil {
			return
		}

		// create cert options
		certOpts := CertOpts{}
		err = ff.GenerateStruct(&certOpts)
		if err != nil {
			return
		}
		TTL, err := time.ParseDuration("800ms")
		if err != nil {
			return
		}
		certOpts.TTL = TTL

		// call target
		ca.Sign(csrPEM, certOpts)
	})
}
