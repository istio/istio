/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package fake

import (
	"bytes"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/webhook/internal/cert/generator"
)

// CertGenerator is a certGenerator for testing.
type CertGenerator struct {
	CAKey                  []byte
	CACert                 []byte
	DNSNameToCertArtifacts map[string]*generator.Artifacts
}

var _ generator.CertGenerator = &CertGenerator{}

// SetCA sets the PEM-encoded CA private key and CA cert for signing the generated serving cert.
func (cp *CertGenerator) SetCA(CAKey, CACert []byte) {
	cp.CAKey = CAKey
	cp.CACert = CACert
}

// Generate generates certificates by matching a common name.
func (cp *CertGenerator) Generate(commonName string) (*generator.Artifacts, error) {
	certs, found := cp.DNSNameToCertArtifacts[commonName]
	if !found {
		return nil, fmt.Errorf("failed to find common name %q in the certGenerator", commonName)
	}
	if cp.CAKey != nil && cp.CACert != nil &&
		!bytes.Contains(cp.CAKey, []byte("invalid")) && !bytes.Contains(cp.CACert, []byte("invalid")) {
		certs.CAKey = cp.CAKey
		certs.CACert = cp.CACert
	}
	return certs, nil
}
