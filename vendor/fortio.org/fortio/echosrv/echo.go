// Copyright 2017 Istio Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

// Initially adapted from istio/proxy/test/backend/echo with error handling and
// concurrency fixes and making it as low overhead as possible
// (no std output by default)

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"fortio.org/fortio/fhttp"
	"fortio.org/fortio/version"
)

var (
	port      = flag.String("port", "8080", "default http port, either port or address:port can be specified")
	debugPath = flag.String("debug-path", "/debug", "path for debug url, set to empty for no debug")
)

func main() {
	flag.Parse()
	if len(os.Args) >= 2 && strings.Contains(os.Args[1], "version") {
		fmt.Println(version.Long())
		os.Exit(0)
	}
	if _, addr := fhttp.Serve(*port, *debugPath); addr == nil {
		os.Exit(1) // error already logged
	}
	select {}
}
