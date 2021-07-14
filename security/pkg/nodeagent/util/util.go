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

package util

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"go.opencensus.io/stats/view"

	"istio.io/istio/pkg/file"
	"istio.io/pkg/env"
)

var k8sInCluster = env.RegisterStringVar("KUBERNETES_SERVICE_HOST", "",
	"Kubernetes service host, set automatically when running in-cluster")

// ParseCertAndGetExpiryTimestamp parses the first certificate in certByte and returns cert expire
// time, or return error if fails to parse certificate.
func ParseCertAndGetExpiryTimestamp(certByte []byte) (time.Time, error) {
	block, _ := pem.Decode(certByte)
	if block == nil {
		return time.Time{}, fmt.Errorf("failed to decode certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse certificate: %v", err)
	}
	return cert.NotAfter, nil
}

// GetMetricsCounterValueWithTags returns counter value in float64. For test purpose only.
func GetMetricsCounterValueWithTags(metricName string, tags map[string]string) (float64, error) {
	rows, err := view.RetrieveData(metricName)
	if err != nil {
		return float64(0), err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	for _, row := range rows {
		need := len(tags)
		for _, t := range row.Tags {
			if tags[t.Key.Name()] == t.Value {
				need--
			}
		}
		if need == 0 {
			return rows[0].Data.(*view.SumData).Value, nil
		}
	}
	return float64(0), fmt.Errorf("no metrics matched tags %s: %d", metricName, len(rows))
}

// Output the key and certificate to the given directory.
// If directory is empty, return nil.
func OutputKeyCertToDir(dir string, privateKey, certChain, rootCert []byte) error {
	if len(dir) == 0 {
		return nil
	}

	certFileMode := os.FileMode(0o600)
	if k8sInCluster.Get() != "" {
		// If this is running on k8s, give more permission to the file certs.
		// This is typically used to share the certs with non-proxy containers in the pod which does not run as root or 1337.
		// For example, prometheus server could use proxy provisioned certs to scrape application metrics through mTLS.
		certFileMode = os.FileMode(0o644)
	}
	// Depending on the SDS resource to output, some fields may be nil
	if privateKey == nil && certChain == nil && rootCert == nil {
		return fmt.Errorf("the input private key, cert chain, and root cert are nil")
	}

	if privateKey != nil {
		if err := file.AtomicWrite(path.Join(dir, "key.pem"), privateKey, certFileMode); err != nil {
			return fmt.Errorf("failed to write private key to file: %v", err)
		}
	}
	if certChain != nil {
		if err := file.AtomicWrite(path.Join(dir, "cert-chain.pem"), certChain, certFileMode); err != nil {
			return fmt.Errorf("failed to write cert chain to file: %v", err)
		}
	}
	if rootCert != nil {
		if err := file.AtomicWrite(path.Join(dir, "root-cert.pem"), rootCert, certFileMode); err != nil {
			return fmt.Errorf("failed to write root cert to file: %v", err)
		}
	}

	return nil
}

type SdsCertificateConfig struct {
	CertificatePath   string
	PrivateKeyPath    string
	CaCertificatePath string
}

const (
	ResourceSeparator = "~"
)

// GetResourceName converts a SdsCertificateConfig to a string to be used as an SDS resource name
func (s SdsCertificateConfig) GetResourceName() string {
	if s.IsKeyCertificate() {
		return "file-cert:" + s.CertificatePath + ResourceSeparator + s.PrivateKeyPath // Format: file-cert:%s~%s
	}
	return ""
}

// GetRootResourceName converts a SdsCertificateConfig to a string to be used as an SDS resource name for the root
func (s SdsCertificateConfig) GetRootResourceName() string {
	if s.IsRootCertificate() {
		return "file-root:" + s.CaCertificatePath // Format: file-root:%s
	}
	return ""
}

// IsRootCertificate returns true if this config represents a root certificate config.
func (s SdsCertificateConfig) IsRootCertificate() bool {
	return s.CaCertificatePath != ""
}

// IsKeyCertificate returns true if this config represents key certificate config.
func (s SdsCertificateConfig) IsKeyCertificate() bool {
	return s.CertificatePath != "" && s.PrivateKeyPath != ""
}

// SdsCertificateConfigFromResourceName converts the provided resource name into a SdsCertificateConfig
// If the resource name is not valid, false is returned.
func SdsCertificateConfigFromResourceName(resource string) (SdsCertificateConfig, bool) {
	if strings.HasPrefix(resource, "file-cert:") {
		filesString := strings.TrimPrefix(resource, "file-cert:")
		split := strings.Split(filesString, ResourceSeparator)
		if len(split) != 2 {
			return SdsCertificateConfig{}, false
		}
		return SdsCertificateConfig{split[0], split[1], ""}, true
	} else if strings.HasPrefix(resource, "file-root:") {
		filesString := strings.TrimPrefix(resource, "file-root:")
		split := strings.Split(filesString, ResourceSeparator)

		if len(split) != 1 {
			return SdsCertificateConfig{}, false
		}
		return SdsCertificateConfig{"", "", split[0]}, true
	} else {
		return SdsCertificateConfig{}, false
	}
}
