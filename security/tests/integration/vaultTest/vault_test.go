// Copyright 2018 Istio Authors
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

package integration

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/adapter/vault"
	"istio.io/istio/security/tests/integration"
	"istio.io/istio/tests/integration/framework"
)

// Config for testing purpose
const (
	// Whether running these tests in the release 0.8
	enableForRelease = true
)

// Config for testing purpose
const (
	testID            = "istio_ca_vault_test"
	testEnvName       = "Istio CA Vault test"
	vaultPort         = 8200
	tokenForTesting   = "myroot"
	testCAKeyCertFile = "testdata/istio_ca.pem"
	testCsrFile       = "testdata/workload-1.csr"
)

// Config for Vault prototyping purpose
const (
	istioCaMountPoint   = "istio_ca"
	istioCaDescription  = "Istio CA"
	configCaKeyCertPath = "istio_ca/config/ca"
	workloadRolePath    = "istio_ca/roles/workload_role"
	signCsrPath         = "istio_ca/sign-verbatim"
)

var (
	testEnv *integration.VaultTestEnv
)

//RunPrototypeSignCsrFlow runs a prototyping signCsr flow, includes:
//- Create a connection to Vault
//- Mount Vault PKI
//- Set CA signing key and cert
//- Set workload role for issuing certificates
//- Sign CSR and print the certificate signed
func RunPrototypeSignCsrFlow(vaultIP string, vaultPort int) error {
	vaultAddr := fmt.Sprintf("http://%s:%d", vaultIP, vaultPort)
	client, err := vault.CreateVaultClientWithToken(vaultAddr, tokenForTesting)
	if err != nil {
		log.Errorf("CreateVaultClientWithToken() failed (error %v)", err)
		return err
	}

	err = vault.MountVaultPki(client, istioCaMountPoint, istioCaDescription)
	if err != nil {
		log.Errorf("MountVaultPki() failed (error %v)", err)
		return err
	}

	keyCert, err := ioutil.ReadFile(testCAKeyCertFile)
	if err != nil {
		log.Errorf("ReadFile() failed (error %v)", err)
		return err
	}
	_, err = vault.SetCaKeyCert(client, configCaKeyCertPath, string(keyCert[:]))
	if err != nil {
		log.Errorf("SetCaKeyCert() failed (error %v)", err)
		return err
	}

	err = vault.SetWorkloadRole(client, workloadRolePath, "1h", 2048)
	if err != nil {
		log.Errorf("SetWorkloadRole() failed (error %v)", err)
		return err
	}

	testCsr, err := ioutil.ReadFile(testCsrFile)
	if err != nil {
		log.Errorf("ReadFile() failed (error %v)", err)
		return err
	}
	res, err := vault.SignCsr(client, signCsrPath, testCsr[:])
	if err != nil {
		log.Errorf("SignCsr() failed (error %v)", err)
		return err
	}
	log.Info("The certificate generated from CSR is :")
	//Print the certificate
	log.Infof("%v", res.Data["certificate"])
	return nil
}

func TestVaultSignCsr(t *testing.T) {
	if !enableForRelease {
		return
	}
	retry := 10
	for i := 0; i < retry; i++ {
		vaultIP, err := testEnv.GetVaultIPAddress()
		if err != nil {
			log.Infof("Failed to get the Vault IP address %v", err)
		}
		time.Sleep(time.Second * 3)
		log.Infof("Attempt (%v) to run the sign CSR flow, retry in 3 seconds\n", i+1)
		err = RunPrototypeSignCsrFlow(vaultIP, vaultPort)
		if err == nil {
			log.Infof("Vault SignCsr() succeeds.")
			return
		}
	}
	t.Errorf("Vault SignCsr() failed.")
}

func TestMain(m *testing.M) {
	if !enableForRelease {
		return
	}
	kubeconfig := flag.String("kube-config", "", "path to kubeconfig file")
	hub := flag.String("hub", "", "Docker hub that the Istio CA image is hosted")
	tag := flag.String("tag", "", "Tag for Istio CA image")

	flag.Parse()

	testEnv = integration.NewVaultTestEnv(testEnvName, *kubeconfig, *hub, *tag)

	if testEnv == nil {
		log.Error("test environment creation failure")
		// There is no cleanup needed at this point.
		os.Exit(1)
	}

	res := framework.NewTestEnvManager(testEnv, testID).RunTest(m)

	log.Infof("Test result %d in env %s", res, testEnvName)

	os.Exit(res)
}
