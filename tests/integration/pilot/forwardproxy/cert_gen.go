//go:build integ

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

package forwardproxy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func GenerateKeyAndCertificate(subject, dir string) (string, string, error) {
	keyFile := filepath.Join(dir, fmt.Sprintf("%s-key.pem", subject))
	crtFile := filepath.Join(dir, fmt.Sprintf("%s-cert.pem", subject))
	if err := openssl(
		"req", "-x509", "-sha256", "-nodes",
		"-days", "365", "-newkey", "rsa:2048",
		"-subj", fmt.Sprintf("/CN=%s", subject),
		"-keyout", keyFile,
		"-out", crtFile,
	); err != nil {
		return "", "", fmt.Errorf("failed to generate private key and certificate: %s", err)
	}
	key, err := os.ReadFile(keyFile)
	if err != nil {
		return "", "", fmt.Errorf("failed to read private key from file %s: %s", keyFile, err)
	}
	crt, err := os.ReadFile(crtFile)
	if err != nil {
		return "", "", fmt.Errorf("failed to read certificate from file %s: %s", crtFile, err)
	}
	return string(key), string(crt), nil
}

func openssl(args ...string) error {
	cmd := exec.Command("openssl", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("command %s failed: %q %v", cmd.String(), string(out), err)
	}
	return nil
}
