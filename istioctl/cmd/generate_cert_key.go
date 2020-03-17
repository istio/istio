// Copyright 2019 Istio Authors.
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

package cmd

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"istio.io/istio/istioctl/pkg/genCert"

	"istio.io/istio/security/pkg/pki/util"

	"k8s.io/client-go/kubernetes"
)

type genCertkeyArgs struct {
	validFrom string
	validFor  time.Duration
	org       string
	outCert   string
	outPriv   string
	keySize   int
	rootCert  string
}

const (
	timeLayout = "Jan 2 15:04:05 2006"
)

var args = &genCertkeyArgs{}

//todo: enhance istioctl manifest apply to auto generate root cert, and multiple intermediate certs, keys and plug them into secrets before installation Istio service mesh
// This command now only supports citadel as CA and the generated cert, key will be used for workloads
// The aim is to provide convenient way for generating cert and key for mesh expansion on VM: https://istio.io/docs/examples/virtual-machines/single-network/
//todo: enhance this command to support k8s as CA when k8s can generate spiffe format cert
func generateKeyAndCertCmd() *cobra.Command {
	generateKeyAndCertCmd := &cobra.Command{
		Use:   "generate-key-cert",
		Short: "Generate key and cert for workloads",
		Example: `  # Generate key and certificate for workload workload-name.vmnamespace and extract root certificate.
  istioctl generate-key-cert  workload <workload-name.namespace>
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("workload name.namespace is required")
			}
			if len(args) > 1 {
				return fmt.Errorf("only workload-name.vmnamespace is supported")
			}
			client, err := interfaceFactory(kubeconfig)
			if err != nil {
				return err
			}
			writer := cmd.OutOrStdout()
			return generateCert(args[0], client, writer)
		},
	}
	generateKeyAndCertCmd.PersistentFlags().StringVar(&args.validFrom, "start-date", "", "Creation date in format of "+timeLayout)
	generateKeyAndCertCmd.PersistentFlags().DurationVar(&args.validFor, "duration", 365*24*time.Hour, "Duration that certificate is valid for.")
	generateKeyAndCertCmd.PersistentFlags().StringVar(&args.org, "organization", "Juju org", "Organization for the cert.")
	generateKeyAndCertCmd.PersistentFlags().StringVar(&args.outCert, "out-cert", "cert-chain.pem", "Output certificate file.")
	generateKeyAndCertCmd.PersistentFlags().StringVar(&args.outPriv, "out-priv", "key.pem", "Output private key file.")
	generateKeyAndCertCmd.PersistentFlags().IntVar(&args.keySize, "key-size", 2048, "Size of the generated private key")
	generateKeyAndCertCmd.PersistentFlags().StringVar(&args.rootCert, "root-cert", "root-cert.pem", "Output root certificate file")
	return generateKeyAndCertCmd
}

func generateCert(name string, client kubernetes.Interface, writer io.Writer) error {
	names := strings.Split(name, ".")
	workloadHost := "spiffee://cluster.local/" + names[1] + "/" + names[0]
	opts := util.CertOptions{
		Host:       workloadHost,
		NotBefore:  genCert.GetNotBefore(args.validFrom),
		TTL:        args.validFor,
		Org:        args.org,
		IsCA:       false,
		IsClient:   true,
		RSAKeySize: args.keySize,
	}
	certPem, privPem, signerCertBytes, err := genCert.GenerateCertKayAndExtractRootCert(opts, client, istioNamespace)
	if err != nil {
		return err
	}
	genCert.SaveCreds(args.outCert, args.outPriv, args.rootCert, certPem, privPem, signerCertBytes)
	_, _ = fmt.Fprintf(writer, "root certificate, Certificate chain and private files successfully saved "+
		"in %q,%q and %q\n", args.rootCert, args.outCert, args.outPriv)
	return nil
}
