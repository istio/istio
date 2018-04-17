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

package workload

import (
	"testing"

	"istio.io/istio/security/pkg/util"
)

func TestNewSecretFileServerConfig(t *testing.T) {
	cert := "certfile"
	key := "keyfile"
	config := NewSecretFileServerConfig(cert, key)

	if config.ServiceIdentityCertFile != cert {
		t.Errorf("cert file does not match")
	}

	if config.ServiceIdentityPrivateKeyFile != key {
		t.Errorf("key file does not match")
	}

	if config.Mode != SecretFile {
		t.Errorf("secret file does not match")
	}

	utilImpl := util.FileUtilImpl{}
	if config.FileUtil != utilImpl {
		t.Errorf("file util does not match")
	}
}
