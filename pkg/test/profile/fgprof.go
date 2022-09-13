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

package profile

import (
	"flag"
	"os"

	"github.com/felixge/fgprof"

	"istio.io/istio/pkg/test"
)

var fprof string

// init initializes additional profiling flags
// Go comes with some, like -cpuprofile and -memprofile by default, so those are elided.
func init() {
	flag.StringVar(&fprof, "fullprofile", "", "enable full profile. Path will be relative to test directory")
}

// FullProfile runs a "Full" profile (https://github.com/felixge/fgprof). This differs from standard
// CPU profile, as it includes both IO blocking and CPU usage in one profile, giving a full view of
// the application.
func FullProfile(t test.Failer) {
	if fprof == "" {
		return
	}
	f, err := os.Create(fprof)
	if err != nil {
		t.Fatalf("%v", err)
	}
	stop := fgprof.Start(f, fgprof.FormatPprof)

	t.Cleanup(func() {
		if err := stop(); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	})
}
