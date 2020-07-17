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

package install

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/coreos/etcd/pkg/fileutil"

	"istio.io/istio/cni/pkg/install-cni/pkg/constants"
	"istio.io/istio/cni/pkg/install-cni/pkg/util"
	"istio.io/pkg/log"
)

func copyBinaries(updateBinaries bool, skipBinaries []string) error {
	srcDir := constants.CNIBinDir
	targetDirs := []string{constants.HostCNIBinDir, constants.SecondaryBinDir}

	skipBinariesSet := arrToSet(skipBinaries)

	for _, targetDir := range targetDirs {
		if fileutil.IsDirWriteable(targetDir) != nil {
			log.Infof("Directory %s is not writable, skipping.", targetDir)
			continue
		}

		files, err := ioutil.ReadDir(srcDir)
		if err != nil {
			return err
		}

		for _, file := range files {
			filename := file.Name()
			if skipBinariesSet[filename] {
				log.Infof("%s is in SKIP_CNI_BINARIES, skipping.", filename)
				continue
			}

			targetFilepath := filepath.Join(targetDir, filename)
			if _, err := os.Stat(targetFilepath); err == nil && !updateBinaries {
				log.Infof("%s is already here and UPDATE_CNI_BINARIES isn't true, skipping", targetFilepath)
				continue
			}

			srcFilepath := filepath.Join(srcDir, filename)
			err := util.AtomicCopy(srcFilepath, targetDir, filename)
			if err != nil {
				return err
			}
			log.Infof("Copied %s to %s.", filename, targetDir)
		}
	}

	return nil
}

func arrToSet(array []string) map[string]bool {
	set := make(map[string]bool)
	for _, v := range array {
		set[v] = true
	}
	return set
}
