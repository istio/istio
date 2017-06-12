// Copyright 2017 Istio Authors
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

package kube

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"testing"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/api/v1"

	"istio.io/pilot/test/util"
)

const (
	controllerCertFile = "testdata/cert.crt"
	controllerKeyFile  = "testdata/cert.key"
)

func TestSecret(t *testing.T) {
	cl := makeClient(t)
	t.Parallel()
	ns, err := util.CreateNamespace(cl)
	if err != nil {
		t.Fatal(err)
	}
	defer util.DeleteNamespace(cl, ns)

	// create the secret
	cert, err := ioutil.ReadFile(controllerCertFile)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ioutil.ReadFile(controllerKeyFile)
	if err != nil {
		t.Fatal(err)
	}

	secret := "istio-secret"
	_, err = cl.Core().Secrets(ns).Create(&v1.Secret{
		ObjectMeta: meta_v1.ObjectMeta{Name: secret},
		Data:       map[string][]byte{secretCert: cert, secretKey: key},
	})
	if err != nil {
		t.Fatal(err)
	}

	eventually(func() bool {
		secret, err := cl.Core().Secrets(ns).Get(secret, meta_v1.GetOptions{})
		return secret != nil && err == nil
	}, t)

	sr := MakeSecretRegistry(cl)

	uri := fmt.Sprintf("%s.%s", secret, ns)
	if tls, err := sr.GetTLSSecret(uri); err != nil {
		t.Error(err)
	} else if tls == nil {
		t.Errorf("GetTLSSecret => no secret")
	} else if !bytes.Equal(cert, tls.Certificate) || !bytes.Equal(key, tls.PrivateKey) {
		t.Errorf("GetTLSSecret => got %q and %q, want %q and %q",
			string(tls.Certificate), string(tls.PrivateKey), string(cert), string(key))
	}
}
