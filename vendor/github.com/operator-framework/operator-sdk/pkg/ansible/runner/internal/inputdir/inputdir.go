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

package inputdir

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/operator-framework/operator-sdk/internal/util/fileutil"
	"github.com/spf13/afero"

	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("inputdir")

// InputDir represents an input directory for ansible-runner.
type InputDir struct {
	Path         string
	PlaybookPath string
	Parameters   map[string]interface{}
	EnvVars      map[string]string
	Settings     map[string]string
}

// makeDirs creates the required directory structure.
func (i *InputDir) makeDirs() error {
	for _, path := range []string{"env", "project", "inventory"} {
		fullPath := filepath.Join(i.Path, path)
		err := os.MkdirAll(fullPath, os.ModePerm)
		if err != nil {
			log.Error(err, "Unable to create directory", "Path", fullPath)
			return err
		}
	}
	return nil
}

// addFile adds a file to the given relative path within the input directory.
func (i *InputDir) addFile(path string, content []byte) error {
	fullPath := filepath.Join(i.Path, path)
	err := ioutil.WriteFile(fullPath, content, 0644)
	if err != nil {
		log.Error(err, "Unable to write file", "Path", fullPath)
	}
	return err
}

// copyInventory copies a file or directory from src to dst
func (i *InputDir) copyInventory(src string, dst string) error {
	fs := afero.NewOsFs()
	return afero.Walk(fs, src,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			fullDst := strings.Replace(path, src, dst, 1)
			if info.IsDir() {
				if err = fs.MkdirAll(fullDst, info.Mode()); err != nil {
					return err
				}
			} else {
				f, err := fs.Open(path)
				if err != nil {
					return err
				}
				if err = afero.WriteReader(fs, fullDst, f); err != nil {
					return err
				}
				if err = fs.Chmod(fullDst, info.Mode()); err != nil {
					return err
				}
			}
			return nil
		})
}

// Stdout reads the stdout from the ansible artifact that corresponds to the
// given ident and returns it as a string.
func (i *InputDir) Stdout(ident string) (string, error) {
	errorPath := filepath.Join(i.Path, "artifacts", ident, "stdout")
	errorText, err := ioutil.ReadFile(errorPath)
	return string(errorText), err
}

// Write commits the object's state to the filesystem at i.Path.
func (i *InputDir) Write() error {
	paramBytes, err := json.Marshal(i.Parameters)
	if err != nil {
		return err
	}
	envVarBytes, err := json.Marshal(i.EnvVars)
	if err != nil {
		return err
	}
	settingsBytes, err := json.Marshal(i.Settings)
	if err != nil {
		return err
	}

	err = i.makeDirs()
	if err != nil {
		return err
	}

	err = i.addFile("env/envvars", envVarBytes)
	if err != nil {
		return err
	}
	err = i.addFile("env/extravars", paramBytes)
	if err != nil {
		return err
	}
	err = i.addFile("env/settings", settingsBytes)
	if err != nil {
		return err
	}

	// ANSIBLE_INVENTORY takes precedence over our generated hosts file
	// so if the envvar is set we don't bother making it, we just copy
	// the inventory into our runner directory
	ansible_inventory := os.Getenv("ANSIBLE_INVENTORY")
	if ansible_inventory == "" {
		// If ansible-runner is running in a python virtual environment, propagate
		// that to ansible.
		venv := os.Getenv("VIRTUAL_ENV")
		hosts := "localhost ansible_connection=local"
		if venv != "" {
			hosts = fmt.Sprintf("%s ansible_python_interpreter=%s", hosts, filepath.Join(venv, "bin/python"))
		}
		err = i.addFile("inventory/hosts", []byte(hosts))
		if err != nil {
			return err
		}
	} else {
		fi, err := os.Stat(ansible_inventory)
		if err != nil {
			return err
		}
		switch mode := fi.Mode(); {
		case mode.IsDir():
			err = i.copyInventory(ansible_inventory, filepath.Join(i.Path, "inventory"))
			if err != nil {
				return err
			}
		case mode.IsRegular():
			err = i.copyInventory(ansible_inventory, filepath.Join(i.Path, "inventory/hosts"))
			if err != nil {
				return err
			}
		}
	}

	if i.PlaybookPath != "" {
		f, err := os.Open(i.PlaybookPath)
		if err != nil {
			log.Error(err, "Failed to open playbook file", "Path", i.PlaybookPath)
			return err
		}
		defer func() {
			if err := f.Close(); err != nil && !fileutil.IsClosedError(err) {
				log.Error(err, "Failed to close playbook file")
			}
		}()

		playbookBytes, err := ioutil.ReadAll(f)
		if err != nil {
			return err
		}

		err = i.addFile("project/playbook.yaml", playbookBytes)
		if err != nil {
			return err
		}
	}
	return nil
}
