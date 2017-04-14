// Copyright 2017 Istio Authors
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

package config

import (
	"io/ioutil"
	"os"
)

// compatStore.go implements compatibility between file based and api based
// configuration.

// NewCompatFSStore creates and returns an fsStore using old style
// This should be removed once we migrate all configs to new style configs.
func NewCompatFSStore(globalConfigFile string, serviceConfigFile string) (KeyValueStore, error) {
	// no configURL, but serviceConfig and globalConfig are specified.
	// provides compatibility
	var err error
	dm := map[string]string{
		keyGlobalServiceConfig: serviceConfigFile,
		keyDescriptors:         globalConfigFile,
	}
	var data []byte
	var dir string
	if dir, err = ioutil.TempDir(os.TempDir(), "fsStore"); err != nil {
		return nil, err
	}
	fs := newFSStore(dir).(*fsStore)

	for k, v := range dm {
		if data, err = fs.readfile(v); err != nil {
			return nil, err
		}
		dm[k] = string(data)
	}
	dm[keyAdapters] = dm[keyDescriptors]

	for k, v := range dm {
		if _, err = fs.Set(k, v); err != nil {
			return nil, err
		}
	}
	return fs, nil
}
