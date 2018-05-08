//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package test

import (
	"flag"
	"fmt"

	"istio.io/istio/pkg/test/impl/driver"
)

var arguments = driver.DefaultArgs()

// init registers the command-line flags that we can exposed for "go test".
func init() {
	// TODO: Register logging flags.

	flag.StringVar(&arguments.Labels, "l", arguments.Labels,
		"Only run tests with the given labels")

	flag.StringVar(&arguments.Environment, "e", arguments.Environment,
		fmt.Sprintf("Specify the environment to run the tests against. Allowed values are: [%s, %s]",
			driver.EnvLocal, driver.EnvKubernetes))

	flag.StringVar(&arguments.KubeConfig, "c", arguments.KubeConfig,
		"The path to the kube config file for cluster environments")
}
