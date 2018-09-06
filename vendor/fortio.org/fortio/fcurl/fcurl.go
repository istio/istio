// Copyright 2018 Istio Authors
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

package main

// Do not add any external dependencies we want to keep fortio minimal.

import (
	"flag"
	"fmt"
	"io"
	"os"

	"fortio.org/fortio/bincommon"
	"fortio.org/fortio/log"
	"fortio.org/fortio/version"
)

// Prints usage
func usage(w io.Writer, msgs ...interface{}) {
	// nolint: gas
	_, _ = fmt.Fprintf(w, "Φορτίο fortio-curl %s usage:\n\t%s [flags] url\n",
		version.Short(),
		os.Args[0])
	bincommon.FlagsUsage(w, msgs...)
}

func main() {
	bincommon.SharedMain(usage)
	if len(os.Args) < 2 {
		usage(os.Stderr, "Error: need a url as parameter")
		os.Exit(1)
	}
	flag.Parse()
	if *bincommon.QuietFlag {
		log.SetLogLevelQuiet(log.Error)
	}
	o := bincommon.SharedHTTPOptions()
	bincommon.FetchURL(o)
}
