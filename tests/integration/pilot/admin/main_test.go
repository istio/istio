//go:build integ

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

package admin

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
)

func TestMain(m *testing.M) {
	disposable := flag.Bool("admin.test.disposable", false, "Confirm that the explicitly selected Kubernetes cluster is disposable")
	flag.Parse()
	kubeconfig := flag.Lookup("istio.test.kube.config")
	if !*disposable || kubeconfig == nil || kubeconfig.Value.String() == "" {
		fmt.Fprintln(os.Stderr, "admin integration tests require -admin.test.disposable=true and an explicit -istio.test.kube.config; suite setup installs and removes Istio")
		os.Exit(2)
	}

	framework.NewSuite(m).Setup(istio.Setup(nil, nil)).Run()
}
