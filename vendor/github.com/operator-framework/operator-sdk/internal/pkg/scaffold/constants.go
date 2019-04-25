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
	"path/filepath"
)

const (
	// Separator to statically create directories.
	filePathSep = string(filepath.Separator)

	// dirs
	CmdDir         = "cmd"
	ManagerDir     = CmdDir + filePathSep + "manager"
	PkgDir         = "pkg"
	ApisDir        = PkgDir + filePathSep + "apis"
	ControllerDir  = PkgDir + filePathSep + "controller"
	BuildDir       = "build"
	BuildTestDir   = BuildDir + filePathSep + "test-framework"
	BuildBinDir    = BuildDir + filePathSep + "_output" + filePathSep + "bin"
	BuildScriptDir = BuildDir + filePathSep + "bin"
	DeployDir      = "deploy"
	OLMCatalogDir  = DeployDir + filePathSep + "olm-catalog"
	CRDsDir        = DeployDir + filePathSep + "crds"
	VersionDir     = "version"
)
