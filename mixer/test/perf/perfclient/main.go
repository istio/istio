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

// Package test supplies a fake Mixer server for use in testing. It should NOT
// be used outside of testing contexts.
package main

import (
	"fmt"
	"os"

	"istio.io/istio/mixer/pkg/perf"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Printf("Args: <controller_address>, <controller_rpc_path>\n")
		return
	}

	address := os.Args[1]
	path := os.Args[2]
	fmt.Printf("Starting external client:\n")
	fmt.Printf("  address:     %s\n", address)
	fmt.Printf("  rpc path: %s\n", path)

	c, err := perf.NewClientServer(perf.ServiceLocation{Address: address, Path: path})
	if err != nil {
		fmt.Printf("Error creating client: %v\n", err)
		return
	}

	c.Wait()
}
