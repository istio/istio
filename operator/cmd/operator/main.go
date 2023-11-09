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

package main

import (
	"os"

	controllruntimelog "sigs.k8s.io/controller-runtime/pkg/log"

	"istio.io/istio/pkg/log"
)

func main() {
	log.EnableKlogWithCobra()
	// adding to remove message about the controller-runtime logs not getting displayed
	scope := log.RegisterScope("controlleruntime", "scope for controller runtime")
	controllruntimelog.SetLogger(log.NewLogrAdapter(scope))

	rootCmd := getRootCmd(os.Args[1:])

	if err := rootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
}
