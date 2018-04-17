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

package main

import (
	"flag"
	"fmt"

	env "istio.io/istio/tests/integration/example/environment/mixerEnvoyEnv"
	"istio.io/istio/tests/integration/framework"
)

func main() {
	flag.Parse()

	testEM := framework.NewTestEnvManager(env.NewMixerEnvoyEnv(""), "")
	if err := testEM.StartUp(); err != nil {
		fmt.Printf("Failed to start the environment: %s\n", err)
	} else {
		fmt.Println("Environment is running")
		for {
			var input string
			fmt.Println("Entry 'exit' to stop the environment, don't use 'ctrl+C'")
			fmt.Scanln(&input)
			if input == "exit" {
				break
			}
		}
	}

	testEM.TearDown()
}
