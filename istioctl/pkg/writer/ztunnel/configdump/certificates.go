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

package configdump

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"text/tabwriter"
	"time"

	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/log"
)

// PrintSecretDump prints just the secret config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintSecretDump(outputFormat string) error {
	if c.ztunnelDump == nil {
		return fmt.Errorf("config writer has not been primed")
	}
	secretDump := c.ztunnelDump.Certificates
	out, err := json.MarshalIndent(secretDump, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal secrets dump: %v", err)
	}
	if outputFormat == "yaml" {
		if out, err = yaml.JSONToYAML(out); err != nil {
			return err
		}
	}
	fmt.Fprintln(c.Stdout, string(out))
	return nil
}

// PrintSecretSummary prints a summary of dynamic active secrets from the config dump
func (c *ConfigWriter) PrintSecretSummary() error {
	if c.ztunnelDump == nil {
		return fmt.Errorf("config writer has not been primed")
	}
	secretDump := c.ztunnelDump.Certificates
	w := new(tabwriter.Writer).Init(c.Stdout, 0, 8, 5, ' ', 0)
	fmt.Fprintln(w, "CERTIFICATE NAME\tTYPE\tSTATUS\tVALID CERT\tSERIAL NUMBER\tNOT AFTER\tNOT BEFORE")

	for _, secret := range secretDump {
		if strings.Contains(secret.State, "Unavailable") {
			secret.State = "Unavailable"
		}
		if len(secret.CertChain) == 0 {
			fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%v\t%v\t%v\n",
				secret.Identity, valueOrNA(""), secret.State, false, valueOrNA(""), valueOrNA(""), valueOrNA(""))
		} else {
			// Before, the root was part of the certChain.
			legacyFormat := len(secret.RootCert) == 0
			for i, ca := range secret.CertChain {
				t := "Intermediate"
				if i == 0 {
					t = "Leaf"
				} else if i == len(secret.CertChain)-1 && legacyFormat {
					t = "Root"
				}
				n := new(big.Int)
				n, _ = n.SetString(ca.SerialNumber, 10)
				fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%x\t%v\t%v\n",
					secret.Identity, t, secret.State, certNotExpired(ca), n, valueOrNA(ca.ExpirationTime), valueOrNA(ca.ValidFrom))
			}
			for _, ca := range secret.RootCert {
				n := new(big.Int)
				n, _ = n.SetString(ca.SerialNumber, 10)
				fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%x\t%v\t%v\n",
					secret.Identity, "Root", secret.State, certNotExpired(ca), n, valueOrNA(ca.ExpirationTime), valueOrNA(ca.ValidFrom))
			}
		}
	}
	return w.Flush()
}

func valueOrNA(value string) string {
	if value == "" {
		return "NA"
	}
	return value
}

func certNotExpired(cert *Cert) bool {
	// case where cert state is in either Initializing or Unavailable state
	if cert.ExpirationTime == "" && cert.ValidFrom == "" {
		return false
	}
	today := time.Now()
	expDate, err := time.Parse(time.RFC3339, cert.ExpirationTime)
	if err != nil {
		log.Errorf("certificate timestamp (%v) could not be parsed: %v", cert.ExpirationTime, err)
		return false
	}
	fromDate, err := time.Parse(time.RFC3339, cert.ValidFrom)
	if err != nil {
		log.Errorf("certificate timestamp (%v) could not be parsed: %v", cert.ValidFrom, err)
		return false
	}
	if today.After(fromDate) && today.Before(expDate) {
		return true
	}
	return false
}
