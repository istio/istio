//  Copyright 2018 Istio Authors
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

package creds

import (
	"path/filepath"

	"github.com/spf13/cobra"
)

// Options defines the credential options required for MCP.
type Options struct {
	// CertificateFile to use for mTLS gRPC.
	CertificateFile string
	// KeyFile to use for mTLS gRPC.
	KeyFile string
	// CACertificateFile is the trusted root certificate authority's cert file.
	CACertificateFile string
}

// DefaultOptions returns default credential options.
func DefaultOptions() *Options {
	return &Options{
		CertificateFile:   filepath.Join(defaultCertDir, defaultCertificateFile),
		KeyFile:           filepath.Join(defaultCertDir, defaultKeyFile),
		CACertificateFile: filepath.Join(defaultCertDir, defaultCACertificateFile),
	}
}

// AttachCobraFlags attaches a set of Cobra flags to the given Cobra command.
//
// Cobra is the command-line processor that Istio uses. This command attaches
// the necessary set of flags to configure the MCP options.
func (c *Options) AttachCobraFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVarP(&c.CertificateFile, "certFile", "", c.CertificateFile,
		"The location of the certificate file for mutual TLS")
	cmd.PersistentFlags().StringVarP(&c.KeyFile, "keyFile", "", c.KeyFile,
		"The location of the key file for mutual TLS")
	cmd.PersistentFlags().StringVarP(&c.CACertificateFile, "caCertFile", "", c.CACertificateFile,
		"The location of the certificate file for the root certificate authority")
}
