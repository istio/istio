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

package clioptions

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// CentralControlPlaneOptions holds options common to all subcommands
// that invoke Istiod via xDS REST endpoint
type CentralControlPlaneOptions struct {
	// Xds is XDS endpoint, e.g. localhost:15010.
	Xds string

	// XdsPodLabel is a Kubernetes label on the Istiod pods
	XdsPodLabel string

	// XdsPodPort is a port exposing XDS (typically 15010 or 15012)
	XdsPodPort int

	// CertDir is the local directory containing certificates
	CertDir string

	// Timeout is how long to wait before giving up on XDS
	Timeout time.Duration

	// InsecureSkipVerify skips client verification the server's certificate chain and host name.
	InsecureSkipVerify bool

	// XDSSAN is the expected Subject Alternative Name of the XDS server
	XDSSAN string
}

// AttachControlPlaneFlags attaches control-plane flags to a Cobra command.
// (Currently just --endpoint)
func (o *CentralControlPlaneOptions) AttachControlPlaneFlags(cmd *cobra.Command) {

	cmd.PersistentFlags().StringVar(&o.Xds, "xds-address", viper.GetString("XDS-ADDRESS"),
		"XDS Endpoint")
	cmd.PersistentFlags().StringVar(&o.CertDir, "cert-dir", viper.GetString("CERT-DIR"),
		"XDS Endpoint certificate directory")
	cmd.PersistentFlags().StringVar(&o.XdsPodLabel, "xds-label", "",
		"Istiod pod label selector")
	cmd.PersistentFlags().IntVar(&o.XdsPodPort, "xds-port", viper.GetInt("XDS-PORT"),
		"Istiod pod port")
	cmd.PersistentFlags().DurationVar(&o.Timeout, "timeout", time.Second*30,
		"the duration to wait before failing")
	cmd.PersistentFlags().StringVar(&o.XDSSAN, "authority", viper.GetString("AUTHORITY"),
		"XDS Subject Alternative Name (for example istiod.istio-system.svc)")
	cmd.PersistentFlags().BoolVar(&o.InsecureSkipVerify, "insecure", viper.GetBool("INSECURE"),
		"Skip server certificate and domain verification. (NOT SECURE!)")
}

// ValidateControlPlaneFlags checks arguments for valid values and combinations
func (o *CentralControlPlaneOptions) ValidateControlPlaneFlags() error {
	if o.Xds != "" && o.XdsPodLabel != "" {
		return fmt.Errorf("either --xds-address or --xds-label, not both")
	}
	return nil
}
