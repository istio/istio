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

package upgrade

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
	"strings"

	"istio.io/istio/pkg/test/env"
)

// Version is the version of the control plane to read installation config for
type Version string

const (
	// NMinusOne should be kept at the tip of the previous minor version
	NMinusOne Version = "1.8.0"
	// NMinusTwo should be kept at the tip of the two minor versions prior
	NMinusTwo Version = "1.7.6"
)

// ToRevision takes a version and turns it into a revision conformant name
func (v Version) ToRevision() string {
	return strings.ReplaceAll(string(v), ".", "-")
}

// ReadInstallFile reads a tar compressed installation file from the embedded
func ReadInstallFile(ver Version) (string, error) {
	f := fmt.Sprintf("%s-install.yaml", ver)
	installFilePath := filepath.Join(env.IstioSrc, "pkg/test/upgrade/testdata", f+".tar")
	b, err := ioutil.ReadFile(installFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read manifest at path %s: %v", installFilePath, err)
	}
	tr := tar.NewReader(bytes.NewBuffer(b))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return "", err
		}
		if hdr.Name != f {
			continue
		}
		contents, err := ioutil.ReadAll(tr)
		if err != nil {
			return "", err
		}
		return string(contents), nil
	}
	return "", fmt.Errorf("file not found in archive")
}
