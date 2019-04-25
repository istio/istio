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

package scaffold

import (
	"bytes"
	"io"
	"os"
	"path/filepath"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"

	log "github.com/sirupsen/logrus"
)

const (
	// test constants describing an app operator project
	appProjectName = "app-operator"
	appRepo        = "github.com" + filePathSep + "example-inc" + filePathSep + appProjectName
	appApiVersion  = "app.example.com/v1alpha1"
	appKind        = "AppService"
)

var (
	appConfig = &input.Config{
		Repo:           appRepo,
		AbsProjectPath: mustGetImportPath(),
		ProjectName:    appProjectName,
	}
)

func mustGetImportPath() string {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get working directory: (%v)", err)
	}
	return filepath.Join(wd, appRepo)
}

func setupScaffoldAndWriter() (*Scaffold, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return &Scaffold{
		GetWriter: func(_ string, _ os.FileMode) (io.Writer, error) {
			return buf, nil
		},
	}, buf
}
