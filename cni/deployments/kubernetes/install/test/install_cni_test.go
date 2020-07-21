// Copyright 2018 Istio Authors
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

package test

import (
	"flag"
	"strings"
	"testing"
)

var (
	chainedCNIPluginFlag = flag.Bool("chainedplugin", true, "chained_plugin")
	preConfFlag          = flag.String("preconf", "", "pre_conf")
	resultFileNameFlag   = flag.String("resultfilename", "", "result_filename")
	delayedConfFlag      = flag.String("delayedconf", "", "delayed_conf")
	expectedConfFlag     = flag.String("expectedconf", "", "expected_conf")
	expectedCleanFlag    = flag.String("expectedclean", "", "expected_clean")
	confDirOrderedFiles  = flag.String("confOrderedFiles", "", "conf_ordered_files")
)

// TestInstallCNI consumes CLI flags and runs the install CNI test.
func TestInstallCNI(t *testing.T) {
	var confFiles []string
	if len(*confDirOrderedFiles) > 0 {
		confFiles = strings.Split(*confDirOrderedFiles, ",")
	}
	RunInstallCNITest(1, *chainedCNIPluginFlag, *preConfFlag, *resultFileNameFlag, *delayedConfFlag, *expectedConfFlag,
		*expectedCleanFlag, confFiles, t)
}
