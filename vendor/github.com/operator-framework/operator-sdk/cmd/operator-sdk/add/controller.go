// Copyright 2018 The Operator-SDK Authors
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

package add

import (
	"fmt"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
	"github.com/operator-framework/operator-sdk/internal/util/projutil"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func newAddControllerCmd() *cobra.Command {
	controllerCmd := &cobra.Command{
		Use:   "controller",
		Short: "Adds a new controller pkg",
		Long: `operator-sdk add controller --kind=<kind> --api-version=<group/version> creates a new
controller pkg under pkg/controller/<kind> that, by default, reconciles on a custom resource for the specified apiversion and kind.
The controller will expect to use the custom resource type that should already be defined under pkg/apis/<group>/<version>
via the "operator-sdk add api --kind=<kind> --api-version=<group/version>" command.
This command must be run from the project root directory.
If the controller pkg for that Kind already exists at pkg/controller/<kind> then the command will not overwrite and return an error.

Example:
	$ operator-sdk add controller --api-version=app.example.com/v1alpha1 --kind=AppService
	$ tree pkg/controller
	pkg/controller/
	├── add_appservice.go
	├── appservice
	│   └── appservice_controller.go
	└── controller.go

`,
		RunE: controllerRun,
	}

	controllerCmd.Flags().StringVar(&apiVersion, "api-version", "", "Kubernetes APIVersion that has a format of $GROUP_NAME/$VERSION (e.g app.example.com/v1alpha1)")
	if err := controllerCmd.MarkFlagRequired("api-version"); err != nil {
		log.Fatalf("Failed to mark `api-version` flag for `add controller` subcommand as required")
	}
	controllerCmd.Flags().StringVar(&kind, "kind", "", "Kubernetes resource Kind name. (e.g AppService)")
	if err := controllerCmd.MarkFlagRequired("kind"); err != nil {
		log.Fatalf("Failed to mark `kind` flag for `add controller` subcommand as required")
	}

	return controllerCmd
}

func controllerRun(cmd *cobra.Command, args []string) error {
	projutil.MustInProjectRoot()

	// Only Go projects can add controllers.
	if err := projutil.CheckGoProjectCmd(cmd); err != nil {
		return err
	}

	log.Infof("Generating controller version %s for kind %s.", apiVersion, kind)

	// Create and validate new resource
	r, err := scaffold.NewResource(apiVersion, kind)
	if err != nil {
		return err
	}

	cfg := &input.Config{
		Repo:           projutil.CheckAndGetProjectGoPkg(),
		AbsProjectPath: projutil.MustGetwd(),
	}

	s := &scaffold.Scaffold{}
	err = s.Execute(cfg,
		&scaffold.ControllerKind{Resource: r},
		&scaffold.AddController{Resource: r},
	)
	if err != nil {
		return fmt.Errorf("controller scaffold failed: (%v)", err)
	}

	log.Info("Controller generation complete.")
	return nil
}
