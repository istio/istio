//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package deps

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"

	"istio.io/istio/pkg/test/env"
)

var (
	Istio []IstioDep
)

func init() {
	content, err := ioutil.ReadFile(filepath.Join(env.IstioSrc, "istio.deps"))
	assert(err)

	Istio = make([]IstioDep, 0)
	assert(json.Unmarshal(content, &Istio))
}

// IstioDep identifies a external dependency of Istio.
type IstioDep struct {
	Comment       string `json:"_comment,omitempty"`
	Name          string `json:"name,omitempty"`
	RepoName      string `json:"repoName,omitempty"`
	LastStableSHA string `json:"lastStableSHA,omitempty"`
}

func assert(err error) {
	if err != nil {
		panic(fmt.Errorf("failed retrieving Envoy SHA: %v", err))
	}
}
