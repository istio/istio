//  Copyright Istio Authors
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

package cert

import (
	"fmt"
	"os/exec"
)

// GenerateKey and writes output to keyFile.
func GenerateKey(keyFile string) error {
	return openssl("genrsa", "-out", keyFile, "4096")
}

// GenerateCSR and writes output to csrFile.
func GenerateCSR(confFile, keyFile, csrFile string) error {
	return openssl("req", "-new",
		"-config", confFile,
		"-key", keyFile,
		"-out", csrFile)
}

// GenerateCert and writes output to certFile.
func GenerateCert(confFile, csrFile, keyFile, certFile string) error {
	return openssl("x509", "-req",
		"-days", "100000",
		"-signkey", keyFile,
		"-extensions", "req_ext",
		"-extfile", confFile,
		"-in", csrFile,
		"-out", certFile)
}

// GenerateIntermediateCert from the rootCA and writes to certFile.
func GenerateIntermediateCert(confFile, csrFile, rootCertFile, rootKeyFile, certFile string) error {
	return openssl("x509", "-req",
		"-days", "100000",
		"-CA", rootCertFile,
		"-CAkey", rootKeyFile,
		"-CAcreateserial",
		"-extensions", "req_ext",
		"-extfile", confFile,
		"-in", csrFile,
		"-out", certFile)
}

func openssl(args ...string) error {
	cmd := exec.Command("openssl", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("command %s failed: %q %v", cmd.String(), string(out), err)
	}
	return nil
}
