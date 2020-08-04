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
	"io/ioutil"
	"path"
	"time"

	"go.opencensus.io/stats/view"
)

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

// GetMetricsCounterValue returns counter value in float64. For test purpose only.
func GetMetricsCounterValue(metricName string) (float64, error) {
	rows, err := view.RetrieveData(metricName)
	if err != nil {
		return float64(0), err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	if len(rows) > 1 {
		return float64(0), fmt.Errorf("unexpected number of data for view %s: %d",
			metricName, len(rows))
	}

	return rows[0].Data.(*view.SumData).Value, nil
}

// Output the key and certificate to the given directory.
// If directory is empty, return nil.
func OutputKeyCertToDir(dir string, privateKey, certChain, rootCert []byte) error {
	if len(dir) == 0 {
		return nil
	}
	// Depending on the SDS resource to output, some fields may be nil
	if privateKey == nil && certChain == nil && rootCert == nil {
		return fmt.Errorf("the input private key, cert chain, and root cert are nil")
	}

	if privateKey != nil {
		if err := ioutil.WriteFile(path.Join(dir, "key.pem"), privateKey, 0777); err != nil {
			return fmt.Errorf("failed to write private key to file: %v", err)
		}
	}
	if certChain != nil {
		if err := ioutil.WriteFile(path.Join(dir, "cert-chain.pem"), certChain, 0777); err != nil {
			return fmt.Errorf("failed to write cert chain to file: %v", err)
		}
	}
	if rootCert != nil {
		if err := ioutil.WriteFile(path.Join(dir, "root-cert.pem"), rootCert, 0777); err != nil {
			return fmt.Errorf("failed to write root cert to file: %v", err)
		}
	}

	return nil
}
