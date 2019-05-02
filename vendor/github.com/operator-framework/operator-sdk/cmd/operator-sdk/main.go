// Copyright 2019 The Operator-SDK Authors
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

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that `run` and `up local` can make use of them.
	"github.com/operator-framework/operator-sdk/cmd/operator-sdk/add"
	"github.com/operator-framework/operator-sdk/cmd/operator-sdk/build"
	"github.com/operator-framework/operator-sdk/cmd/operator-sdk/completion"
	"github.com/operator-framework/operator-sdk/cmd/operator-sdk/generate"
	"github.com/operator-framework/operator-sdk/cmd/operator-sdk/migrate"
	"github.com/operator-framework/operator-sdk/cmd/operator-sdk/new"
	"github.com/operator-framework/operator-sdk/cmd/operator-sdk/olmcatalog"
	"github.com/operator-framework/operator-sdk/cmd/operator-sdk/printdeps"
	"github.com/operator-framework/operator-sdk/cmd/operator-sdk/run"
	"github.com/operator-framework/operator-sdk/cmd/operator-sdk/scorecard"
	"github.com/operator-framework/operator-sdk/cmd/operator-sdk/test"
	"github.com/operator-framework/operator-sdk/cmd/operator-sdk/up"
	"github.com/operator-framework/operator-sdk/cmd/operator-sdk/version"
	osdkversion "github.com/operator-framework/operator-sdk/version"
	"github.com/spf13/cobra"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

func main() {
	root := &cobra.Command{
		Use:     "operator-sdk",
		Short:   "An SDK for building operators with ease",
		Version: osdkversion.Version,
	}

	root.AddCommand(new.NewCmd())
	root.AddCommand(add.NewCmd())
	root.AddCommand(build.NewCmd())
	root.AddCommand(generate.NewCmd())
	root.AddCommand(up.NewCmd())
	root.AddCommand(completion.NewCmd())
	root.AddCommand(test.NewCmd())
	root.AddCommand(scorecard.NewCmd())
	root.AddCommand(printdeps.NewCmd())
	root.AddCommand(migrate.NewCmd())
	root.AddCommand(run.NewCmd())
	root.AddCommand(olmcatalog.NewCmd())
	root.AddCommand(version.NewCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
