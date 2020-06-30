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
	"github.com/spf13/viper"

	"istio.io/istio/cni/pkg/install-cni/pkg/constants"
	"istio.io/pkg/log"
)

func copyBinaries() error {
	srcDir := "/opt/cni/bin"
	targetDirs := []string{constants.HostCniBinDir, constants.SecondaryBinDir}

	for _, targetDir := range targetDirs {
		if fileutil.IsDirWriteable(targetDir) != nil {
			log.Infof("Directory %s is not writable, skipping.", targetDir)
			continue
		}

		files, err := ioutil.ReadDir(srcDir)
		if err != nil {
			return err
		}

		skipBinaries := viper.GetStringSlice(constants.SkipCniBinaries)
		for _, file := range files {
			filename := file.Name()
			if contains(skipBinaries, filename) {
				log.Infof("%s is in SKIP_CNI_BINARIES, skipping.", filename)
				continue
			}

			targetFilename := filepath.Join(targetDir, filename)
			if _, err := os.Stat(targetFilename); err == nil && !viper.GetBool(constants.UpdateCniBinaries) {
				log.Infof("%s is already here and UPDATE_CNI_BINARIES isn't true, skipping", targetFilename)
				continue
			}

			srcFilename := filepath.Join(srcDir, filename)
			err := copy(srcFilename, targetDir, filename)
			if err != nil {
				return err
			}
			log.Infof("Copied %s to %s.", filename, targetDir)
		}
	}

	return nil
}

func contains(array []string, value string) bool {
	for _, s := range array {
		if s == value {
			return true
		}
	}
	return false
}

// Copy files atomically by first copying into the same directory then renaming.
func copy(src, targetDir, targetFilename string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	input, err := ioutil.ReadFile(src)
	if err != nil {
		return err
	}

	tmpFile, err := ioutil.TempFile(targetDir, "CNI_TEMP_")
	if err != nil {
		return err
	}
	if err = tmpFile.Chmod(info.Mode()); err != nil {
		return err
	}
	if _, err = tmpFile.Write(input); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err = tmpFile.Close(); err != nil {
		return err
	}

	return os.Rename(tmpFile.Name(), filepath.Join(targetDir, targetFilename))
}
