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

package plugin

import (
	"context"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc/metadata"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/spiffe"
	"istio.io/pkg/log"
)

type TokenRequestPlugin struct {
	audience         string
	client           kube.Client
	identityProvider string
	delegate         security.CredFetcher
}

var _ security.CredFetcher = &TokenRequestPlugin{}

func fileExists(path string) bool {
	if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() {
		return true
	}
	return false
}

const (
	kindKeyCert = "/var/lib/kubelet/pki/kubelet-client-current.pem"
	gkeKey      = "/var/lib/kubelet/pki/kubelet-client.key"
	gkeCert     = "/var/lib/kubelet/pki/kubelet-client.crt"
)

func TokenRequest(audience, identityProvider string, delegate security.CredFetcher) (*TokenRequestPlugin, error) {
	kubeRestConfig, err := kube.DefaultRestConfig("", "")
	if err != nil {
		return nil, err
	}
	if fileExists(kindKeyCert) {
		kubeRestConfig.TLSClientConfig.CertFile = kindKeyCert
		kubeRestConfig.TLSClientConfig.KeyFile = kindKeyCert
	} else if fileExists(gkeCert) {
		kubeRestConfig.TLSClientConfig.CertFile = gkeCert
		kubeRestConfig.TLSClientConfig.KeyFile = gkeKey
	}
	client, err := kube.NewClient(kube.NewClientConfigForRestConfig(kubeRestConfig))
	if err != nil {
		return nil, err
	}

	return &TokenRequestPlugin{
		audience:         audience,
		identityProvider: identityProvider,
		client:           client,
		delegate:         delegate,
	}, nil
}

func (t TokenRequestPlugin) GetPlatformCredential(ctx context.Context) (string, error) {
	// TODO: do this only for custom ones, not all
	mx, f := metadata.FromOutgoingContext(ctx)
	if !f {
		return "", fmt.Errorf("no san found")
	}
	uids := mx.Get("UID")
	if len(uids) != 1 || uids[0] == "" {
		log.Infof("use delegate provider")
		// Not something we should request, fallback
		return t.delegate.GetPlatformCredential(ctx)
	}

	var ref *authenticationv1.BoundObjectReference
	if len(uids) == 1 && uids[0] != "" {
		ref = &authenticationv1.BoundObjectReference{
			Kind:       "Pod",
			APIVersion: "v1",
			Name:       mx.Get("POD")[0],
			UID:        types.UID(uids[0]),
		}
	}
	sans := mx.Get("SAN")
	if len(sans) != 1 {
		return "", fmt.Errorf("no san found: %v", sans)
	}
	san := sans[0]
	sp, _ := spiffe.ParseIdentity(san)
	dur := int64((time.Hour * 24).Seconds())
	token := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			Audiences:         []string{"istio-ca"},
			ExpirationSeconds: &dur,
			BoundObjectRef:    ref,
		},
	}
	tok, err := t.client.Kube().CoreV1().ServiceAccounts(sp.Namespace).CreateToken(context.Background(), sp.ServiceAccount, token, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("could not create a token for %v/%v: %v", sp, ref, err)
	}
	log.Infof("created token for %v: %v", sp, tok.Status.Token)
	return tok.Status.Token, nil
}

func (t TokenRequestPlugin) GetType() string {
	return security.TokenRequest
}

func (t TokenRequestPlugin) GetIdentityProvider() string {
	return t.identityProvider
}

func (t TokenRequestPlugin) Stop() {
}
