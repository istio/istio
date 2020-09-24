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

package main

import (
	"io/ioutil"
	"os"
	"path"
	"time"

	"github.com/spf13/cobra"

	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/security/pkg/adapter/vault"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

const (
	// The environmental variable name for the directory to write data retrieved from Vault.
	envOutputDir          = "OUTPUT_DIR"
	envClientCertFileName = "CLIENT_CERT_FILENAME"
	envClientKeyFileName  = "CLIENT_KEY_FILENAME"
	envServerCertFileName = "SERVER_CERT_FILENAME"
	envPinFileName        = "PIN_FILENAME"
)

var (
	outputDir = env.RegisterStringVar(envOutputDir, "/var/run/vaultdata",
		"The directory path to write the Vault data").Get()
	clientCertFileName = env.RegisterStringVar(envClientCertFileName, "clientcert.pem",
		"The cleint cert file in the vault output directory").Get()
	clientKeyFileName = env.RegisterStringVar(envClientKeyFileName, "clientkey.pem",
		"The cleint key file in the vault output directory").Get()
	serverCertFileName = env.RegisterStringVar(envServerCertFileName, "servercert.pem",
		"The server cert file in the vault output directory").Get()
	pinFileName = env.RegisterStringVar(envPinFileName, "pin",
		"The PIN file in the vault output directory").Get()
)

func main() {
	if err := cmd().Execute(); err != nil {
		log.Errora(err)
		os.Exit(-1)
	}
}

func cmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "vault_client",
		Short: "ASM Vault client util binary.",
		Long:  "This command interacts with Vault server to retrieve secrets and write them into the local file.",
		Run: func(cmd *cobra.Command, args []string) {
			runVaultClient()
		},
	}
	rootCmd.AddCommand(version.CobraCommand())

	return rootCmd
}

func runVaultClient() {
	cs, err := kubelib.CreateClientset("", "")
	if err != nil {
		log.Fatalf("Failed to create k8s clientset: %v", err)
	}
	client, err := vault.NewVaultClient(cs.CoreV1())
	if err != nil {
		log.Fatalf("Failed to create Vaultclient: %v", err)
	}
	clientCertPem, clientKeyPem, serverCertPem, pin, err := client.GetHSMCredentilals()
	if err != nil {
		log.Fatalf("Failed to retrieve values from Vault: %v", err)
	}
	clientCertFilePath := path.Join(outputDir, clientCertFileName)
	if err := ioutil.WriteFile(clientCertFilePath, clientCertPem, 0644); err != nil {
		log.Fatalf("Failed to write client cert to file: %s", err)
	}
	clientKeyFilePath := path.Join(outputDir, clientKeyFileName)
	if err := ioutil.WriteFile(clientKeyFilePath, clientKeyPem, 0644); err != nil {
		log.Fatalf("Failed to write client key to file: %s", err)
	}
	serverCertFilePath := path.Join(outputDir, serverCertFileName)
	if err := ioutil.WriteFile(serverCertFilePath, serverCertPem, 0644); err != nil {
		log.Fatalf("Failed to write server cert to file: %s", err)
	}
	pinFilePath := path.Join(outputDir, pinFileName)
	if err := ioutil.WriteFile(pinFilePath, pin, 0644); err != nil {
		log.Fatalf("Failed to write PIN to file: %s", err)
	}
	log.Infof("Successfully written data from Vault to files in %v", outputDir)

	// We may not need this. The process is done after the file is written.
	for {
		time.Sleep(time.Minute)
	}
}
