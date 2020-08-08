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

package cache

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/nodeagent/cache/mock"
	"istio.io/pkg/filewatcher"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/istio/security/pkg/nodeagent/secretfetcher"
	nodeagentutil "istio.io/istio/security/pkg/nodeagent/util"

	"github.com/fsnotify/fsnotify"
)

var (
	k8sKey       = []byte("fake private k8sKey")
	k8sCertChain = []byte(`-----BEGIN CERTIFICATE-----
MIIFPzCCAyegAwIBAgIDEAISMA0GCSqGSIb3DQEBCwUAMEQxCzAJBgNVBAYTAlVT
MQ8wDQYDVQQIDAZEZW5pYWwxDDAKBgNVBAoMA0RpczEWMBQGA1UEAwwNKi5leGFt
cGxlLmNvbTAeFw0xOTA1MjkyMzEzMjFaFw0yOTA1MjYyMzEzMjFaMFoxCzAJBgNV
BAYTAlVTMQ8wDQYDVQQIDAZEZW5pYWwxFDASBgNVBAcMC1NwcmluZ2ZpZWxkMQww
CgYDVQQKDANEaXMxFjAUBgNVBAMMDSouZXhhbXBsZS5jb20wggEiMA0GCSqGSIb3
DQEBAQUAA4IBDwAwggEKAoIBAQC7M6icT8vcZ3KZpTZW3zOUGBpJCRG5HKfclyZ7
ONY9Dnc/3+nESJ0vqIOnV7/NbqjWoJYQ8v1xoUPRvAKA6nVSXmvHEfOuq0LofYFP
DoC0o6WWi6VslZEOtf87jzcGLbLbBkBLbmd5/LWp5zw4Bexe0YHKSnsObcCphleN
DeCaPA73Z9YzDEL53PLyH0emuXKyx+lsDcijv+ualVu4HACpeo354Df3XzyxH3TT
5+sLLBVf4ZsyFsP/VtZYwP9PCWepgPH0l1ekPN7AE8MQQmddJUjTFepnRaVMvwmt
dWirp/S1TVYawGLIN/SlX+BHvKPvcd0GXxKm8AjdHD2q7ZxtAgMBAAGjggEiMIIB
HjAJBgNVHRMEAjAAMBEGCWCGSAGG+EIBAQQEAwIGQDAzBglghkgBhvhCAQ0EJhYk
T3BlblNTTCBHZW5lcmF0ZWQgU2VydmVyIENlcnRpZmljYXRlMB0GA1UdDgQWBBQW
ljUOEibJ9/dJ8dw1UOV8Wcg18TCBhAYDVR0jBH0we4AU44b/zezM4Uz3S6WXtXN+
cQoj46WhXqRcMFoxCzAJBgNVBAYTAlVTMQ8wDQYDVQQIDAZEZW5pYWwxFDASBgNV
BAcMC1NwcmluZ2ZpZWxkMQwwCgYDVQQKDANEaXMxFjAUBgNVBAMMDSouZXhhbXBs
ZS5jb22CAxACEjAOBgNVHQ8BAf8EBAMCBaAwEwYDVR0lBAwwCgYIKwYBBQUHAwEw
DQYJKoZIhvcNAQELBQADggIBABpv2bmPbl3meDpLQ2cSH9QoDitkYBMxwp7cL6GA
4Orv6SPbE+PIiijpSSMKbbtMgliL8eJwxAjyPUms0djtH2hvDLZXnRmCVHm4+SOy
CSU7PofELigd1B9w9BRQ183AWzClvtKoS/fVq8szHtLVy+5rgj6bigskdis6lnyN
NzcXusi2Rp+BmcrNRsfin223dbrEG5qeJqTOniEtHyrIzFUYUK9RGtdSVq/k4+0m
DQuG9yK7oqrhL5aSzIs89RD5ofDadhBZJx6OukIppO3aqDJPt8JKI72hhaJc0YNG
CgbluqC2CCZK0BA9JB9c4WGDwr+yvmXxXTO/z5Tk27Pac3vVyEGV4Se5bG1ZT5it
ctsLyKXW/GMWe55T3A5u/0ebvIM7VRquGq4khFq/Rm5uRJhkYBG2j40SHSoIZGV7
bsQR2ucH//EOyWHN96JGrkKV9QaeK/+kV0u7rvqTbzMDKjp2dH0UxTEE7ToHczam
iNNYj8XTzQ7BsqQY5Ti1N2MKxfzgtmik+3BEP42pr9RS9d4PCE/SxF6DOEcP9nZc
5FbxAztiqK6WRZoXegLKEYylA/kT6BKk/M42jJ7SWH76f+Breylo28w+XRX01DPu
3YtkgFZMmdf6fdZLgo3uOUP2aBUZYneHNMDDmt+/VqyykwpHLBeeJ4cAXikmWQQY
rMx/
-----END CERTIFICATE-----`)
	k8sCertChainExpireTime, _ = nodeagentutil.ParseCertAndGetExpiryTimestamp(k8sCertChain)

	k8sCaCert = []byte(`-----BEGIN CERTIFICATE-----
MIIFgTCCA2mgAwIBAgIDEAISMA0GCSqGSIb3DQEBCwUAMFoxCzAJBgNVBAYTAlVT
MQ8wDQYDVQQIDAZEZW5pYWwxFDASBgNVBAcMC1NwcmluZ2ZpZWxkMQwwCgYDVQQK
DANEaXMxFjAUBgNVBAMMDSouZXhhbXBsZS5jb20wHhcNMTkwNTI5MjMxMzE5WhcN
MzkwNTI0MjMxMzE5WjBEMQswCQYDVQQGEwJVUzEPMA0GA1UECAwGRGVuaWFsMQww
CgYDVQQKDANEaXMxFjAUBgNVBAMMDSouZXhhbXBsZS5jb20wggIiMA0GCSqGSIb3
DQEBAQUAA4ICDwAwggIKAoICAQDH9KuhyhG+uhwXda576gUnklaAmQNDmAA/NBP+
qbx3MQIwDcfthikVaEvHPs1i6Roh8x35svenyq6ig2JE6Db77mq6/AvyrEuB2itw
aeB20KqVxm2jKc8gQMiJOoYzaTElceCLXSwM2zxxefiXkk7UcyH8o3tWHyQHuGqB
nqQa2FD/mlP6Jf4vxVnYVjab4k8ytFxqvWQGIqr1+qw3lY0dpNFetvSX0JUYzrtB
2RB5l1n6GeFfBBgzi8cLmBNLdHP8fpjb6J3uo2vGzMFKGAIP4MdO56XEPTYqXTxm
0Iw5uAOSf8ZxFhSToUZoOCPE3EKQh7EUyKn+bJSvDiIcXKwTMBMqoaLefx0yTfPS
hkv1wWMVZ4uLGvJ2epgSLMk3vTKycDMa1qVAL5+Kl4+W39LU/mq3I5NSATZVvwrk
SMGouLAMfc14hMfi/qw4vIhvxrulnkCkNdoLY75suUhaOgcvCeLD7XaBgeglfmCk
Mn7ewI+dPkEgQqbqSy72nul5URubNO5e+fQ5LlwoOUX22iA0YJ/98KrutWMIq1GB
+ZM23ZPzgL1OQOgPBzf3GacyWoQMZl57apJPVUeMoakuqdiVefR0w3LxsMXusXIs
FgtNIpj7/i7vc+CSnd1yp2cUFe3Lagg9jflSNEanR/rQoTxyOnjl4Q4zxtrhTIND
ylqDcQIDAQABo2YwZDAdBgNVHQ4EFgQU44b/zezM4Uz3S6WXtXN+cQoj46UwHwYD
VR0jBBgwFoAUVQpCveVxgNI3RhgPgillGF75pwkwEgYDVR0TAQH/BAgwBgEB/wIB
ADAOBgNVHQ8BAf8EBAMCAYYwDQYJKoZIhvcNAQELBQADggIBAAO1OOyIMjcFqBWB
svy/2lg87mGRhVnOlx8eeDw47Xrq2H665EgTxKefwpCe5w2vWvijiPenag6BGqbH
M7bwVoMf5i9ETtUihsFybST7QRQV8uaDk/uL5mLgZ5qucM+Uk1P3Y7Y4JiRxvLLn
0FRKTWfvQnql3KLdeHQ0nYKoiHIlLfrcVI9hDxHEs75ZJ/4OHMzyUDK+idQu2UXW
nY6Mydh1oMQ07vfbuzix7OntJB5/aP+XO5cTfqhUE68sXFHOuJrEidHO+Ec3nLib
kEN1hOhN2z3PGOisyg4GVrbz24y5NxEhE8qfFFjBDdHHLw42EjuZtVBIh0Ubh5d9
jxN1QUW0MdH0B3prb4D+ptab9LxZLe0prfv/iDqfVgtDZUrZG2xaqe9/aX85SMLn
4nx42zDBnNkgRaVAc5oC/WT4IhbJG2YlWZ4ZymvoLl4ZrKms983qcg6cK++MZYz/
ggC4eqSXqApQ99FA/j/c9Is3iMuWArhpVqe+sNjcD3+Fudhra91GxYAr6R+HubJV
hFI0ryZ+QphOSQMPi+ai5SWidwu2k7nzCwIQ7IkBBlwnjpVsZYtPXOdb4vf36ojU
rc8cdnePDG810YTvyE+nullZVnvQ4biHckIueMLdcxvg0H7BwI4rUFHxwckyRrHO
0kR1qoe1f8jDvCzcFeImPQ2BR4FW
-----END CERTIFICATE-----`)
	k8sCaCertExpireTime, _ = nodeagentutil.ParseCertAndGetExpiryTimestamp(k8sCaCert)

	k8sGenericSecretName = "test-generic-scrt"
	k8sTestGenericSecret = &v1.Secret{
		Data: map[string][]byte{
			"cert":   k8sCertChain,
			"key":    k8sKey,
			"cacert": k8sCaCert,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sGenericSecretName,
			Namespace: "test-namespace",
		},
		Type: "test-generic-secret",
	}
	k8sTLSSecretName = "test-tls-scrt"
	k8sTestTLSSecret = &v1.Secret{
		Data: map[string][]byte{
			"tls.crt": k8sCertChain,
			"tls.key": k8sKey,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sTLSSecretName,
			Namespace: "test-namespace",
		},
		Type: "test-tls-secret",
	}
	k8sTLSFallbackSecretName      = "fallback-scrt"
	k8sTLSFallbackSecretKey       = []byte("fallback fake private key")
	k8sTLSFallbackSecretCertChain = []byte(`-----BEGIN CERTIFICATE-----
MIIFPzCCAyegAwIBAgIDEAISMA0GCSqGSIb3DQEBCwUAMEQxCzAJBgNVBAYTAlVT
MQ8wDQYDVQQIDAZEZW5pYWwxDDAKBgNVBAoMA0RpczEWMBQGA1UEAwwNKi5leGFt
cGxlLmNvbTAeFw0xOTA1MjkyMzE0NDJaFw0yOTA1MjYyMzE0NDJaMFoxCzAJBgNV
BAYTAlVTMQ8wDQYDVQQIDAZEZW5pYWwxFDASBgNVBAcMC1NwcmluZ2ZpZWxkMQww
CgYDVQQKDANEaXMxFjAUBgNVBAMMDSouZXhhbXBsZS5jb20wggEiMA0GCSqGSIb3
DQEBAQUAA4IBDwAwggEKAoIBAQC2VeRleiVMqw/1zcMZrWJmML0nTdYaNliI7WIj
wQvZ+k05D469nClWg7vZ4whOK9lxIHzjWyxSC5FUFqX+NrKm3XbFo9/9u33GCjdO
2gaTr/zCter0a0kPsL5n9KwUkXseRMcrQyV39vl6c9Oecnr1LBhIvg1Z9iwFZsgS
aE8/NMZbsFALQx6EdiltG8lmz4jNPMAd2XS0MnKyO4EGXkkOvkthhf5kJ4KoGtVa
fhZUyActPeUYnaEspXxHE3ANerZPJs+Waykzt9oXpgplyQAmEPTzX3EttCLs0qH7
ZpdceYsOxSm06ea1G46yMzEoQMH5OLkmjfeohngDITa8ELy9AgMBAAGjggEiMIIB
HjAJBgNVHRMEAjAAMBEGCWCGSAGG+EIBAQQEAwIGQDAzBglghkgBhvhCAQ0EJhYk
T3BlblNTTCBHZW5lcmF0ZWQgU2VydmVyIENlcnRpZmljYXRlMB0GA1UdDgQWBBQ+
DNEDMyYWywiYdD8or9ScBq2JdDCBhAYDVR0jBH0we4AURfQW0owHXQLtnkMvKDSx
CY4Wy5qhXqRcMFoxCzAJBgNVBAYTAlVTMQ8wDQYDVQQIDAZEZW5pYWwxFDASBgNV
BAcMC1NwcmluZ2ZpZWxkMQwwCgYDVQQKDANEaXMxFjAUBgNVBAMMDSouZXhhbXBs
ZS5jb22CAxACEjAOBgNVHQ8BAf8EBAMCBaAwEwYDVR0lBAwwCgYIKwYBBQUHAwEw
DQYJKoZIhvcNAQELBQADggIBAAv98L5UBqLla4cpLQJQnW/iz9U0biLYRc80JLJY
50kZ2gsQeGxAd3XLzlgjh95z64JCaFO2XRcpNlEvJ0knAQby8UzBEvDYUDJXlG0p
4WDXndZDqXq3xMs3HHADT42KOLXKu1xcPZ/JyInLdZXnB4lMDIiPk2Kb/w6J+aIV
/jMMosxoEiOr1Rzv34xRNys/HpxFsItwJwUmMzXyM1eLDQPnvklt52va3WdZw6Ki
N1+KCxrbKCxBhTB1itvOAHX3KC7UTt0k0fP7pDc6lranc3hmDRgGIBy5vW+hrtZy
BGMJ/9jT+LWaBBAhhWQOYT0ILC/FmgUipC6kzB6KJrcDsrAxyr+lCxB0qy2HBtnm
s/geO3f6Z7cXLirwNi2SY6+D2jNbguJzvX43x/UNa4qUvXRi5tj/TwDth9M6jEnH
6ml+13liI8QX4+79ODgePt6VjxR71+KSd+51Qcwn1oajr80KoI0NhuuP4ZCBV53K
5Njz6XvJsPMgeVAIRmt3P+gOCW8kQrX6Qgb1fIvSrqR7vLpmybRQcvPt+k/T3m1F
VTkSsST68aOXCmHFLNA/6QB/OUatQYSr+O5iGz99xPEyz4EvHN5ZqTf51d+kyUje
zXsMLXBLCRwYz2U8y1FCcdOvCqDixneworIrBYDmoSafGUceSGQZOCN6ajW7fujg
FWy1
-----END CERTIFICATE-----`)
	k8sTLSFallbackSecretCertChainExpireTime, _ = nodeagentutil.ParseCertAndGetExpiryTimestamp(
		k8sTLSFallbackSecretCertChain)
	k8sTestTLSFallbackSecret = &v1.Secret{
		Data: map[string][]byte{
			"tls.crt": k8sTLSFallbackSecretCertChain,
			"tls.key": k8sTLSFallbackSecretKey,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sTLSFallbackSecretName,
			Namespace: "test-namespace",
		},
		Type: "test-tls-secret",
	}
	fakeExpiredToken = "eyJhbGciOiJSUzI1NiIsImtpZCI6Ik5adXVfam5Zdm5xQ19ZbU54aXRycV" +
		"gyVWo3cmZkaFplcEF2QUJpUmxmLXMifQ.eyJhdWQiOlsiaXN0aW8tY2EiXSwiZXhwIjoxNTk2Nz" +
		"Y5MTYyLCJpYXQiOjE1OTY3Njg1NjIsImlzcyI6Imh0dHBzOi8vY29udGFpbmVyLmdvb2dsZWFwa" +
		"XMuY29tL3YxL3Byb2plY3RzL3dpbGxpYW1saWlzdGlvdGVzdC9sb2NhdGlvbnMvdXMtd2VzdDIt" +
		"YS9jbHVzdGVycy92bWRlYnVnIiwia3ViZXJuZXRlcy5pbyI6eyJuYW1lc3BhY2UiOiJ2bXRlc3Q" +
		"iLCJwb2QiOnsibmFtZSI6InNsZWVwLWY4Y2JmNWI3Ni14cHBteiIsInVpZCI6IjFkMDFkMWYwLW" +
		"VkN2UtNDVhZS1iNTAzLTFhZjAwMDMyODFkZiJ9LCJzZXJ2aWNlYWNjb3VudCI6eyJuYW1lIjoic" +
		"2xlZXAiLCJ1aWQiOiIzYmI3ZjJkYi1mMTEzLTQxYmItOTg3OC0yYTNhODlhYjNlMjYifX0sIm5i" +
		"ZiI6MTU5Njc2ODU2Miwic3ViIjoic3lzdGVtOnNlcnZpY2VhY2NvdW50OnZtdGVzdDpzbGVlcCJ" +
		"9.Ekw9liKDDQgK-RSzIfb2fM0ZH6vQeBKBfA9LuH_3UcCQzKSXV_l-qST3GMmBcmrbAF8MZLcNA" +
		"bzlwZ_lcWDAYGvHG89fA4OKSZsvG0d83_nuP9hx39qqXSdrJ_OJMTGXXHcvapCgFFMPVa59KDCe" +
		"aHkbTjmn6v3NpJ78_Cq8VxpNBKZkrdxEZOKfyula7tWegneWwJN7r0XWKB4A6hefMV8ouGI9p0N" +
		"JujQG96_RkbY4dMeii3Z45mI6CtnAcWZ2ph8bO-OJz83KiGwtzfWvLjrPYpo-vcS7TuPNgALFgB" +
		"SfH9S2mnmv1eJuiAYj4HkWM0K0_GK3wIBMe-YxBEmd4w"
)

func TestWorkloadAgentGenerateSecretWithoutPluginProvider(t *testing.T) {
	testWorkloadAgentGenerateSecret(t, false)
}

func TestWorkloadAgentGenerateSecretWithPluginProvider(t *testing.T) {
	testWorkloadAgentGenerateSecret(t, true)
}

func testWorkloadAgentGenerateSecret(t *testing.T, isUsingPluginProvider bool) {
	// The mocked CA client returns 2 errors before returning a valid response.
	fakeCACli, err := mock.NewMockCAClient(2, time.Hour)
	if err != nil {
		t.Fatalf("Error creating Mock CA client: %v", err)
	}
	opt := &security.Options{
		RotationInterval: 100 * time.Millisecond,
		EvictionDuration: 0,
	}

	if isUsingPluginProvider {
		// The mocked token exchanger server returns 3 errors before returning a valid response.
		fakePlugin := mock.NewMockTokenExchangeServer(3)
		opt.TokenExchangers = []security.TokenExchanger{fakePlugin}
	}

	fetcher := &secretfetcher.SecretFetcher{
		UseCaClient: true,
		CaClient:    fakeCACli,
	}
	sc := NewSecretCache(fetcher, notifyCb, opt)
	defer func() {
		sc.Close()
	}()

	checkBool(t, "opt.SkipParseToken default", opt.SkipParseToken, false)

	conID := "proxy1-id"
	ctx := context.Background()
	gotSecret, err := sc.GenerateSecret(ctx, conID, WorkloadKeyCertResourceName, "jwtToken1")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}

	if got, want := gotSecret.CertificateChain, []byte(strings.Join(fakeCACli.GeneratedCerts[0], "")); !bytes.Equal(got, want) {
		t.Errorf("Got unexpected certificate chain #1. Got: %v, want: %v", string(got), string(want))
	}

	checkBool(t, "SecretExist", sc.SecretExist(conID, WorkloadKeyCertResourceName, "jwtToken1", gotSecret.Version), true)
	checkBool(t, "SecretExist", sc.SecretExist(conID, WorkloadKeyCertResourceName, "nonexisttoken", gotSecret.Version), false)

	gotSecretRoot, err := sc.GenerateSecret(ctx, conID, RootCertReqResourceName, "jwtToken1")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	// Root cert is the last element in the generated certs.
	if got, want := gotSecretRoot.RootCert, []byte(fakeCACli.GeneratedCerts[0][2]); !bytes.Equal(got, want) {
		t.Errorf("Got unexpected root certificate. Got: %v\n want: %v", string(got), string(want))
	}

	checkBool(t, "SecretExist", sc.SecretExist(conID, RootCertReqResourceName, "jwtToken1", gotSecretRoot.Version), true)
	checkBool(t, "SecretExist", sc.SecretExist(conID, RootCertReqResourceName, "nonexisttoken", gotSecretRoot.Version), false)

	if got, want := atomic.LoadUint64(&sc.rootCertChangedCount), uint64(0); got != want {
		t.Errorf("Got unexpected rootCertChangedCount: Got: %v\n want: %v", got, want)
	}

	// Try to get secret again using different jwt token, verify secret is re-generated.
	gotSecret, err = sc.GenerateSecret(ctx, conID, WorkloadKeyCertResourceName, "newToken")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}

	if got, want := gotSecret.CertificateChain, []byte(strings.Join(fakeCACli.GeneratedCerts[1], "")); !bytes.Equal(got, want) {
		t.Errorf("Got unexpected certificate chain #2. Got: %v, want: %v", string(got), string(want))
	}

	// Root cert stays the same.
	if got, want := atomic.LoadUint64(&sc.rootCertChangedCount), uint64(0); got != want {
		t.Errorf("Got unexpected rootCertChangedCount: Got: %v\n want: %v", got, want)
	}
}

func TestWorkloadAgentRefreshSecret(t *testing.T) {
	fakeCACli, err := mock.NewMockCAClient(0, time.Millisecond)
	if err != nil {
		t.Fatalf("Error creating Mock CA client: %v", err)
	}
	opt := &security.Options{
		RotationInterval: 100 * time.Millisecond,
		EvictionDuration: 0,
	}
	fetcher := &secretfetcher.SecretFetcher{
		UseCaClient: true,
		CaClient:    fakeCACli,
	}
	sc := NewSecretCache(fetcher, notifyCb, opt)
	defer func() {
		sc.Close()
	}()

	testConnID := "proxy1-id"
	_, err = sc.GenerateSecret(context.Background(), testConnID, WorkloadKeyCertResourceName, "jwtToken1")
	if err != nil {
		t.Fatalf("Failed to get secrets for %q: %v", testConnID, err)
	}

	// Wait until key rotation job run to update cached secret.
	wait := 200 * time.Millisecond
	retries := 0
	for ; retries < 5; retries++ {
		time.Sleep(wait)
		if atomic.LoadUint64(&sc.secretChangedCount) == uint64(0) {
			// Retry after some sleep.
			wait *= 2
			continue
		}

		break
	}
	if retries == 5 {
		t.Errorf("Cached secret failed to get refreshed, %d", atomic.LoadUint64(&sc.secretChangedCount))
	}

	key := ConnKey{
		ConnectionID: testConnID,
		ResourceName: WorkloadKeyCertResourceName,
	}
	if _, found := sc.secrets.Load(key); !found {
		t.Errorf("Failed to find secret for %+v from cache", key)
	}

	sc.DeleteSecret(testConnID, WorkloadKeyCertResourceName)
	if _, found := sc.secrets.Load(key); found {
		t.Errorf("Found deleted secret for %+v from cache", key)
	}
}

// TestGatewayAgentGenerateSecret verifies that ingress gateway agent manages secret cache correctly.
func TestGatewayAgentGenerateSecret(t *testing.T) {
	sc := createSecretCache()
	fetcher := sc.fetcher
	defer func() {
		sc.Close()
	}()

	connID1 := "proxy1-id"
	connID2 := "proxy2-id"
	ctx := context.Background()

	type expectedSecret struct {
		exist  bool
		secret *security.SecretItem
	}

	cases := []struct {
		addSecret       *v1.Secret
		connID          string
		expectedSecrets []expectedSecret
	}{
		{
			addSecret: k8sTestGenericSecret,
			connID:    connID1,
			expectedSecrets: []expectedSecret{
				{
					exist: true,
					secret: &security.SecretItem{
						ResourceName:     k8sGenericSecretName,
						CertificateChain: k8sCertChain,
						ExpireTime:       k8sCertChainExpireTime,
						PrivateKey:       k8sKey,
					},
				},
				{
					exist: true,
					secret: &security.SecretItem{
						ResourceName: k8sGenericSecretName + "-cacert",
						RootCert:     k8sCaCert,
						ExpireTime:   k8sCaCertExpireTime,
					},
				},
			},
		},
		{
			addSecret: k8sTestTLSSecret,
			connID:    connID2,
			expectedSecrets: []expectedSecret{
				{
					exist: true,
					secret: &security.SecretItem{
						ResourceName:     k8sTLSSecretName,
						CertificateChain: k8sCertChain,
						ExpireTime:       k8sCertChainExpireTime,
						PrivateKey:       k8sKey,
					},
				},
				{
					exist: false,
					secret: &security.SecretItem{
						ResourceName: k8sTLSSecretName + "-cacert",
					},
				},
			},
		},
	}

	for _, c := range cases {
		fetcher.AddSecret(c.addSecret)
		for _, es := range c.expectedSecrets {
			gotSecret, err := sc.GenerateSecret(ctx, c.connID, es.secret.ResourceName, "")
			if es.exist {
				if err != nil {
					t.Fatalf("Failed to get secrets: %v", err)
				}
				if err := verifySecret(gotSecret, es.secret); err != nil {
					t.Errorf("Secret verification failed: %v", err)
				}
				checkBool(t, "SecretExist", sc.SecretExist(c.connID, es.secret.ResourceName, "", gotSecret.Version), true)
				checkBool(t, "SecretExist", sc.SecretExist(c.connID, "nonexistsecret", "", gotSecret.Version), false)
			}
			key := ConnKey{
				ConnectionID: c.connID,
				ResourceName: es.secret.ResourceName,
			}
			cachedSecret, found := sc.secrets.Load(key)
			if es.exist {
				if !found {
					t.Errorf("Failed to find secret for proxy %q from secret store: %v", c.connID, err)
				}
				if !reflect.DeepEqual(*gotSecret, cachedSecret) {
					t.Errorf("Secret key: got %+v, want %+v", *gotSecret, cachedSecret)
				}
			}
			if _, err := sc.GenerateSecret(ctx, c.connID, "nonexistk8ssecret", ""); err == nil {
				t.Error("Generating secret using a non existing kubernetes secret should fail")
			}
		}
	}

	// Wait until unused secrets are evicted.
	wait := 500 * time.Millisecond
	retries := 0
	for ; retries < 3; retries++ {
		time.Sleep(wait)
		if _, found := sc.secrets.Load(connID1); found {
			// Retry after some sleep.
			wait *= 2
			continue
		}
		if _, found := sc.secrets.Load(connID2); found {
			// Retry after some sleep.
			wait *= 2
			continue
		}

		break
	}
	if retries == 3 {
		t.Errorf("Unused secrets failed to be evicted from cache")
	}
}

// TestGatewayAgentGenerateSecretUsingFallbackSecret verifies that ingress gateway agent picks
// fallback secret for ingress gateway and serves real secret when that secret is ready.
func TestGatewayAgentGenerateSecretUsingFallbackSecret(t *testing.T) {
	os.Setenv("INGRESS_GATEWAY_FALLBACK_SECRET", k8sTLSFallbackSecretName)
	sc := createSecretCache()
	fetcher := sc.fetcher
	if fetcher.FallbackSecretName != k8sTLSFallbackSecretName {
		t.Errorf("Fallback secret name does not match. Expected %v but got %v",
			k8sTLSFallbackSecretName, fetcher.FallbackSecretName)
	}
	defer func() {
		sc.Close()
	}()

	connID1 := "proxy1-id"
	connID2 := "proxy2-id"
	ctx := context.Background()

	type expectedSecret struct {
		exist  bool
		secret *security.SecretItem
	}

	cases := []struct {
		addSecret        *v1.Secret
		connID           string
		expectedFbSecret expectedSecret
		expectedSecrets  []expectedSecret
	}{
		{
			addSecret: k8sTestGenericSecret,
			connID:    connID1,
			expectedFbSecret: expectedSecret{
				exist: true,
				secret: &security.SecretItem{
					ResourceName:     k8sGenericSecretName,
					CertificateChain: k8sTLSFallbackSecretCertChain,
					ExpireTime:       k8sTLSFallbackSecretCertChainExpireTime,
					PrivateKey:       k8sTLSFallbackSecretKey,
				},
			},
			expectedSecrets: []expectedSecret{
				{
					exist: true,
					secret: &security.SecretItem{
						ResourceName:     k8sGenericSecretName,
						CertificateChain: k8sCertChain,
						ExpireTime:       k8sCertChainExpireTime,
						PrivateKey:       k8sKey,
					},
				},
				{
					exist: true,
					secret: &security.SecretItem{
						ResourceName: k8sGenericSecretName + "-cacert",
						RootCert:     k8sCaCert,
						ExpireTime:   k8sCaCertExpireTime,
					},
				},
			},
		},
		{
			addSecret: k8sTestTLSSecret,
			connID:    connID2,
			expectedFbSecret: expectedSecret{
				exist: true,
				secret: &security.SecretItem{
					ResourceName:     k8sTLSSecretName,
					CertificateChain: k8sTLSFallbackSecretCertChain,
					ExpireTime:       k8sTLSFallbackSecretCertChainExpireTime,
					PrivateKey:       k8sTLSFallbackSecretKey,
				},
			},
			expectedSecrets: []expectedSecret{
				{
					exist: true,
					secret: &security.SecretItem{
						ResourceName:     k8sTLSSecretName,
						CertificateChain: k8sCertChain,
						ExpireTime:       k8sCertChainExpireTime,
						PrivateKey:       k8sKey,
					},
				},
				{
					exist: false,
					secret: &security.SecretItem{
						ResourceName: k8sTLSSecretName + "-cacert",
					},
				},
			},
		},
	}

	fetcher.AddSecret(k8sTestTLSFallbackSecret)
	for _, c := range cases {
		if sc.ShouldWaitForGatewaySecret(c.connID, c.expectedFbSecret.secret.ResourceName, "", false) {
			t.Fatal("When fallback secret is enabled, node agent should not wait for gateway secret")
		}
		// Verify that fallback secret is returned
		gotSecret, err := sc.GenerateSecret(ctx, c.connID, c.expectedFbSecret.secret.ResourceName, "")
		if err != nil {
			t.Fatalf("Failed to get fallback secrets: %v", err)
		}
		if err := verifySecret(gotSecret, c.expectedFbSecret.secret); err != nil {
			t.Errorf("Secret verification failed: %v", err)
		}
		if got, want := sc.SecretExist(c.connID, c.expectedFbSecret.secret.ResourceName, "", gotSecret.Version), true; got != want {
			t.Errorf("SecretExist: got: %v, want: %v", got, want)
		}
		if got, want := sc.SecretExist(c.connID, "nonexistsecret", "", gotSecret.Version), false; got != want {
			t.Errorf("SecretExist: got: %v, want: %v", got, want)
		}

		key := ConnKey{
			ConnectionID: c.connID,
			ResourceName: c.expectedFbSecret.secret.ResourceName,
		}
		cachedSecret, found := sc.secrets.Load(key)

		if !found {
			t.Errorf("Failed to find secret for proxy %q from secret store: %v", c.connID, err)
		}
		if !reflect.DeepEqual(*gotSecret, cachedSecret) {
			t.Errorf("Secret key: got %+v, want %+v", *gotSecret, cachedSecret)
		}

		// When real secret is added, verify that real secret is returned.
		fetcher.AddSecret(c.addSecret)
		for _, es := range c.expectedSecrets {
			gotSecret, err := sc.GenerateSecret(ctx, c.connID, es.secret.ResourceName, "")
			if es.exist {
				if err != nil {
					t.Fatalf("Failed to get secrets: %v", err)
				}
				if err := verifySecret(gotSecret, es.secret); err != nil {
					t.Errorf("Secret verification failed: %v", err)
				}
				if got, want := sc.SecretExist(c.connID, es.secret.ResourceName, "", gotSecret.Version), true; got != want {
					t.Errorf("SecretExist: got: %v, want: %v", got, want)
				}
				if got, want := sc.SecretExist(c.connID, "nonexistsecret", "", gotSecret.Version), false; got != want {
					t.Errorf("SecretExist: got: %v, want: %v", got, want)
				}
			}
			key := ConnKey{
				ConnectionID: c.connID,
				ResourceName: es.secret.ResourceName,
			}
			cachedSecret, found := sc.secrets.Load(key)
			if es.exist {
				if !found {
					t.Errorf("Failed to find secret for proxy %q from secret store: %v", c.connID, err)
				}
				if !reflect.DeepEqual(*gotSecret, cachedSecret) {
					t.Errorf("Secret key: got %+v, want %+v", *gotSecret, cachedSecret)
				}
			}
		}
		// When secret is deleted, node agent should not wait for gateway secret.
		fetcher.DeleteSecret(c.addSecret)
		if sc.ShouldWaitForGatewaySecret(c.connID, c.expectedFbSecret.secret.ResourceName, "", false) {
			t.Fatal("When fallback secret is enabled, node agent should not wait for gateway secret")
		}
	}

	// Wait until unused secrets are evicted.
	wait := 500 * time.Millisecond
	retries := 0
	for ; retries < 3; retries++ {
		time.Sleep(wait)
		if _, found := sc.secrets.Load(connID1); found {
			// Retry after some sleep.
			wait *= 2
			continue
		}
		if _, found := sc.secrets.Load(connID2); found {
			// Retry after some sleep.
			wait *= 2
			continue
		}

		break
	}
	if retries == 3 {
		t.Errorf("Unused secrets failed to be evicted from cache")
	}
}

func createSecretCache() *SecretCache {
	fetcher := &secretfetcher.SecretFetcher{
		UseCaClient: false,
	}
	fetcher.FallbackSecretName = "gateway-fallback"
	if fallbackSecret := os.Getenv("INGRESS_GATEWAY_FALLBACK_SECRET"); fallbackSecret != "" {
		fetcher.FallbackSecretName = fallbackSecret
	}

	fetcher.InitWithKubeClient(fake.NewSimpleClientset().CoreV1())
	ch := make(chan struct{})
	fetcher.Run(ch)
	opt := &security.Options{
		RotationInterval: 100 * time.Millisecond,
		EvictionDuration: 0,
	}
	return NewSecretCache(fetcher, notifyCb, opt)
}

// Validate that file mounted certs do not wait for ingress secret.
func TestShouldWaitForGatewaySecretForFileMountedCerts(t *testing.T) {
	fetcher := &secretfetcher.SecretFetcher{
		UseCaClient: false,
	}
	opt := &security.Options{
		RotationInterval: 100 * time.Millisecond,
		EvictionDuration: 0,
	}
	sc := NewSecretCache(fetcher, notifyCb, opt)
	if sc.ShouldWaitForGatewaySecret("", "", "", true) {
		t.Fatalf("Expected not to wait for gateway secret for file mounted certs, but got true")
	}
}

// TestGatewayAgentDeleteSecret verifies that ingress gateway agent deletes secret cache correctly.
func TestGatewayAgentDeleteSecret(t *testing.T) {
	sc := createSecretCache()
	fetcher := sc.fetcher
	defer func() {
		sc.Close()
	}()

	fetcher.AddSecret(k8sTestGenericSecret)
	fetcher.AddSecret(k8sTestTLSSecret)
	connID := "proxy1-id"
	ctx := context.Background()
	gotSecret, err := sc.GenerateSecret(ctx, connID, k8sGenericSecretName, "")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sGenericSecretName, "", gotSecret.Version), true)

	gotSecret, err = sc.GenerateSecret(ctx, connID, k8sGenericSecretName+"-cacert", "")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sGenericSecretName+"-cacert", "", gotSecret.Version), true)

	gotSecret, err = sc.GenerateSecret(ctx, connID, k8sTLSSecretName, "")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sTLSSecretName, "", gotSecret.Version), true)

	_, err = sc.GenerateSecret(ctx, connID, k8sTLSSecretName+"-cacert", "")
	if err == nil {
		t.Fatalf("Get unexpected secrets: %v", err)
	}
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sTLSSecretName+"-cacert", "", gotSecret.Version), false)

	sc.DeleteK8sSecret(k8sGenericSecretName)
	sc.DeleteK8sSecret(k8sGenericSecretName + "-cacert")
	sc.DeleteK8sSecret(k8sTLSSecretName)
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sGenericSecretName, "", gotSecret.Version), false)
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sGenericSecretName+"-cacert", "", gotSecret.Version), false)
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sTLSSecretName, "", gotSecret.Version), false)
}

// TestGatewayAgentUpdateSecret verifies that ingress gateway agent updates secret cache correctly.
func TestGatewayAgentUpdateSecret(t *testing.T) {
	sc := createSecretCache()
	fetcher := sc.fetcher
	defer func() {
		sc.Close()
	}()

	fetcher.AddSecret(k8sTestGenericSecret)
	connID := "proxy1-id"
	ctx := context.Background()
	gotSecret, err := sc.GenerateSecret(ctx, connID, k8sGenericSecretName, "")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sGenericSecretName, "", gotSecret.Version), true)
	gotSecret, err = sc.GenerateSecret(ctx, connID, k8sGenericSecretName+"-cacert", "")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sGenericSecretName+"-cacert", "", gotSecret.Version), true)
	newTime := gotSecret.CreatedTime.Add(time.Duration(10) * time.Second)
	newK8sTestSecret := security.SecretItem{
		CertificateChain: []byte("new cert chain"),
		PrivateKey:       []byte("new private key"),
		RootCert:         []byte("new root cert"),
		ResourceName:     k8sGenericSecretName,
		Token:            gotSecret.Token,
		CreatedTime:      newTime,
		Version:          newTime.String(),
	}
	sc.UpdateK8sSecret(k8sGenericSecretName, newK8sTestSecret)
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sGenericSecretName, "", gotSecret.Version), false)
	sc.UpdateK8sSecret(k8sGenericSecretName+"-cacert", newK8sTestSecret)
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sGenericSecretName+"-cacert", "", gotSecret.Version), false)
}

func TestConstructCSRHostName(t *testing.T) {
	data, err := ioutil.ReadFile("./testdata/testjwt")
	if err != nil {
		t.Errorf("failed to read test jwt file %v", err)
	}
	testJwt := string(data)

	cases := []struct {
		trustDomain string
		token       string
		expected    string
		errFlag     bool
	}{
		{
			token:    testJwt,
			expected: "spiffe://cluster.local/ns/default/sa/sleep",
			errFlag:  false,
		},
		{
			trustDomain: "fooDomain",
			token:       testJwt,
			expected:    "spiffe://fooDomain/ns/default/sa/sleep",
			errFlag:     false,
		},
		{
			token:    "faketoken",
			expected: "",
			errFlag:  true,
		},
	}
	for _, c := range cases {
		got, err := constructCSRHostName(c.trustDomain, c.token)
		if err != nil {
			if c.errFlag == false {
				t.Errorf("constructCSRHostName no error, but got %v", err)
			}
			continue
		}

		if c.errFlag == true {
			t.Error("constructCSRHostName error")
		}

		if got != c.expected {
			t.Errorf("constructCSRHostName got %q, want %q", got, c.expected)
		}
	}
}

func checkBool(t *testing.T, name string, got bool, want bool) {
	if got != want {
		t.Errorf("%s: got: %v, want: %v", name, got, want)
	}
}

func TestParseTokenFlag(t *testing.T) {
	sc := createSecretCache()
	defer sc.Close()
	sc.configOptions.SkipParseToken = true
	secret := security.SecretItem{}
	checkBool(t, "isTokenExpired", sc.isTokenExpired(&secret), false)
}

func TestExpiredToken(t *testing.T) {
	sc := createSecretCache()
	defer sc.Close()
	sc.configOptions.SkipParseToken = false
	secret := security.SecretItem{}
	secret.Token = fakeExpiredToken
	checkBool(t, "isTokenExpired", sc.isTokenExpired(&secret), true)
}

func TestRootCertificateExists(t *testing.T) {
	testCases := map[string]struct {
		certPath     string
		expectResult bool
	}{
		"cert not exist": {
			certPath:     "./invalid-path/invalid-file",
			expectResult: false,
		},
		"cert valid": {
			certPath:     "./testdata/cert-chain.pem",
			expectResult: true,
		},
	}

	sc := createSecretCache()
	defer sc.Close()
	for _, tc := range testCases {
		ret := sc.rootCertificateExist(tc.certPath)
		if tc.expectResult != ret {
			t.Errorf("unexpected result is returned!")
		}
	}
}

func TestKeyCertificateExist(t *testing.T) {
	testCases := map[string]struct {
		certPath     string
		keyPath      string
		expectResult bool
	}{
		"cert not exist": {
			certPath:     "./invalid-path/invalid-file",
			keyPath:      "./testdata/cert-chain.pem",
			expectResult: false,
		},
		"key not exist": {
			certPath:     "./testdata/cert-chain.pem",
			keyPath:      "./invalid-path/invalid-file",
			expectResult: false,
		},
		"key and cert valid": {
			certPath:     "./testdata/cert-chain.pem",
			keyPath:      "./testdata/cert-chain.pem",
			expectResult: true,
		},
	}

	sc := createSecretCache()
	defer sc.Close()
	for _, tc := range testCases {
		ret := sc.keyCertificateExist(tc.certPath, tc.keyPath)
		if tc.expectResult != ret {
			t.Errorf("unexpected result is returned!")
		}
	}
}

func notifyCb(_ ConnKey, _ *security.SecretItem) error {
	return nil
}

// TestWorkloadAgentGenerateSecretFromFile tests generating secrets from existing files on a
// secretcache instance.
func TestWorkloadAgentGenerateSecretFromFile(t *testing.T) {
	fakeCACli, err := mock.NewMockCAClient(0, time.Hour)
	if err != nil {
		t.Fatalf("Error creating Mock CA client: %v", err)
	}
	opt := &security.Options{
		RotationInterval: 200 * time.Millisecond,
		EvictionDuration: 0,
		UseTokenForCSR:   true,
		SkipParseToken:   false,
	}

	fetcher := &secretfetcher.SecretFetcher{
		UseCaClient: true,
		CaClient:    fakeCACli,
	}

	var wgAddedWatch sync.WaitGroup
	var notifyEvent sync.WaitGroup

	addedWatchProbe := func(_ string, _ bool) { wgAddedWatch.Done() }

	var closed bool
	notifyCallback := func(_ ConnKey, _ *security.SecretItem) error {
		if !closed {
			notifyEvent.Done()
		}
		return nil
	}

	// Supply a fake watcher so that we can watch file events.
	var fakeWatcher *filewatcher.FakeWatcher
	newFileWatcher, fakeWatcher = filewatcher.NewFakeWatcher(addedWatchProbe)

	sc := NewSecretCache(fetcher, notifyCallback, opt)
	defer func() {
		closed = true
		sc.Close()
		newFileWatcher = filewatcher.NewWatcher
	}()

	rootCertPath := "./testdata/root-cert.pem"
	keyPath := "./testdata/key.pem"
	certChainPath := "./testdata/cert-chain.pem"
	sc.existingRootCertFile = rootCertPath
	sc.existingKeyFile = keyPath
	sc.existingCertChainFile = certChainPath
	certchain, err := ioutil.ReadFile(certChainPath)
	if err != nil {
		t.Fatalf("Error reading the cert chain file: %v", err)
	}
	privateKey, err := ioutil.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("Error reading the private key file: %v", err)
	}
	rootCert, err := ioutil.ReadFile(rootCertPath)
	if err != nil {
		t.Fatalf("Error reading the root cert file: %v", err)
	}

	conID := "proxy1-id"
	ctx := context.Background()

	wgAddedWatch.Add(1) // Watch should be added for cert file.
	notifyEvent.Add(1)  // Nofify should be called once.

	gotSecret, err := sc.GenerateSecret(ctx, conID, WorkloadKeyCertResourceName, "jwtToken1")

	wgAddedWatch.Wait()
	notifyEvent.Wait()

	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	checkBool(t, "SecretExist", sc.SecretExist(conID, WorkloadKeyCertResourceName, "jwtToken1", gotSecret.Version), true)
	expectedSecret := &security.SecretItem{
		ResourceName:     WorkloadKeyCertResourceName,
		CertificateChain: certchain,
		PrivateKey:       privateKey,
	}
	if err := verifySecret(gotSecret, expectedSecret); err != nil {
		t.Errorf("Secret verification failed: %v", err)
	}

	wgAddedWatch.Add(1) // Watch should be added for root file.
	notifyEvent.Add(1)  // Notify should be called once.

	gotSecretRoot, err := sc.GenerateSecret(ctx, conID, RootCertReqResourceName, "jwtToken1")

	wgAddedWatch.Wait()
	notifyEvent.Wait()

	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	checkBool(t, "SecretExist", sc.SecretExist(conID, RootCertReqResourceName, "jwtToken1", gotSecretRoot.Version), true)
	if got, want := atomic.LoadUint64(&sc.rootCertChangedCount), uint64(0); got != want {
		t.Errorf("rootCertChangedCount: got: %v, want: %v", got, want)
	}

	rootExpiration, err := nodeagentutil.ParseCertAndGetExpiryTimestamp(rootCert)
	if err != nil {
		t.Fatalf("Failed to get the expiration time from the existing root file")
	}
	expectedSecret = &security.SecretItem{
		ResourceName: RootCertReqResourceName,
		RootCert:     rootCert,
		ExpireTime:   rootExpiration,
	}
	if err := verifyRootCASecret(gotSecretRoot, expectedSecret); err != nil {
		t.Errorf("Secret verification failed: %v", err)
	}

	key := ConnKey{
		ConnectionID: conID,
		ResourceName: WorkloadKeyCertResourceName,
	}
	cachedSecret, found := sc.secrets.Load(key)
	if !found {
		t.Errorf("Failed to find secret for proxy %q from secret store: %v", conID, err)
	}
	if !reflect.DeepEqual(*gotSecret, cachedSecret) {
		t.Errorf("Secret key: got %+v, want %+v", *gotSecret, cachedSecret)
	}

	// Inject a file write event and validate that Notify is called.
	notifyEvent.Add(1)
	fakeWatcher.InjectEvent(certChainPath, fsnotify.Event{
		Name: certChainPath,
		Op:   fsnotify.Write,
	})
	notifyEvent.Wait()
}

// TestWorkloadAgentGenerateSecretFromFileOverSds tests generating secrets from existing files on a
// secretcache instance, specified over SDS.
func TestWorkloadAgentGenerateSecretFromFileOverSds(t *testing.T) {
	fetcher := &secretfetcher.SecretFetcher{}

	opt := &security.Options{
		RotationInterval: 200 * time.Millisecond,
		EvictionDuration: 0,
	}

	var wgAddedWatch sync.WaitGroup
	var notifyEvent sync.WaitGroup
	var closed bool

	addedWatchProbe := func(_ string, _ bool) { wgAddedWatch.Done() }

	notifyCallback := func(_ ConnKey, _ *security.SecretItem) error {
		if !closed {
			notifyEvent.Done()
		}
		return nil
	}

	// Supply a fake watcher so that we can watch file events.
	var fakeWatcher *filewatcher.FakeWatcher
	newFileWatcher, fakeWatcher = filewatcher.NewFakeWatcher(addedWatchProbe)

	sc := NewSecretCache(fetcher, notifyCallback, opt)
	defer func() {
		closed = true
		sc.Close()
		newFileWatcher = filewatcher.NewWatcher
	}()
	rootCertPath, _ := filepath.Abs("./testdata/root-cert.pem")
	keyPath, _ := filepath.Abs("./testdata/key.pem")
	certChainPath, _ := filepath.Abs("./testdata/cert-chain.pem")
	certchain, err := ioutil.ReadFile(certChainPath)
	if err != nil {
		t.Fatalf("Error reading the cert chain file: %v", err)
	}
	privateKey, err := ioutil.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("Error reading the private key file: %v", err)
	}
	rootCert, err := ioutil.ReadFile(rootCertPath)
	if err != nil {
		t.Fatalf("Error reading the root cert file: %v", err)
	}

	resource := fmt.Sprintf("file-cert:%s~%s", certChainPath, keyPath)
	conID := "proxy1-id"
	ctx := context.Background()

	// Since we do not have rotation enabled, we do not get secret notification.
	wgAddedWatch.Add(1) // Watch should be added for cert file.

	gotSecret, err := sc.GenerateSecret(ctx, conID, resource, "jwtToken1")

	wgAddedWatch.Wait()

	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	checkBool(t, "SecretExist", sc.SecretExist(conID, resource, "jwtToken1", gotSecret.Version), true)
	expectedSecret := &security.SecretItem{
		ResourceName:     resource,
		CertificateChain: certchain,
		PrivateKey:       privateKey,
	}
	if err := verifySecret(gotSecret, expectedSecret); err != nil {
		t.Errorf("Secret verification failed: %v", err)
	}

	rootResource := "file-root:" + rootCertPath

	wgAddedWatch.Add(1) // Watch should be added for root file.

	gotSecretRoot, err := sc.GenerateSecret(ctx, conID, rootResource, "jwtToken1")

	wgAddedWatch.Wait()

	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	checkBool(t, "SecretExist", sc.SecretExist(conID, rootResource, "jwtToken1", gotSecretRoot.Version), true)
	if got, want := atomic.LoadUint64(&sc.rootCertChangedCount), uint64(0); got != want {
		t.Errorf("rootCertChangedCount: got: %v, want: %v", got, want)
	}

	rootExpiration, err := nodeagentutil.ParseCertAndGetExpiryTimestamp(rootCert)
	if err != nil {
		t.Fatalf("Failed to get the expiration time from the existing root file")
	}
	expectedSecret = &security.SecretItem{
		ResourceName: rootResource,
		RootCert:     rootCert,
		ExpireTime:   rootExpiration,
	}
	if err := verifyRootCASecret(gotSecretRoot, expectedSecret); err != nil {
		t.Errorf("Secret verification failed: %v", err)
	}

	key := ConnKey{
		ConnectionID: conID,
		ResourceName: resource,
	}
	cachedSecret, found := sc.secrets.Load(key)
	if !found {
		t.Errorf("Failed to find secret for proxy %q from secret store: %v", conID, err)
	}
	if !reflect.DeepEqual(*gotSecret, cachedSecret) {
		t.Errorf("Secret key: got %+v, want %+v", *gotSecret, cachedSecret)
	}

	// Inject a file write event and validate that Notify is called.
	notifyEvent.Add(1)
	fakeWatcher.InjectEvent(certChainPath, fsnotify.Event{
		Name: certChainPath,
		Op:   fsnotify.Write,
	})
	notifyEvent.Wait()
}

func TestWorkloadAgentGenerateSecretFromFileOverSdsWithBogusFiles(t *testing.T) {
	fetcher := &secretfetcher.SecretFetcher{}
	originalTimeout := totalTimeout
	totalTimeout = time.Second * 1
	defer func() {
		totalTimeout = originalTimeout
	}()

	opt := &security.Options{
		RotationInterval: 1 * time.Millisecond,
		EvictionDuration: 0,
	}
	sc := NewSecretCache(fetcher, notifyCb, opt)
	defer func() {
		sc.Close()
	}()
	rootCertPath, _ := filepath.Abs("./testdata/root-cert-bogus.pem")
	keyPath, _ := filepath.Abs("./testdata/key-bogus.pem")
	certChainPath, _ := filepath.Abs("./testdata/cert-chain-bogus.pem")

	resource := fmt.Sprintf("file-cert:%s~%s", certChainPath, keyPath)
	conID := "proxy1-id"
	ctx := context.Background()

	gotSecret, err := sc.GenerateSecret(ctx, conID, resource, "jwtToken1")

	if err == nil {
		t.Fatalf("expected to get error")
	}

	if gotSecret != nil {
		t.Fatalf("Expected to get nil secret but got %v", gotSecret)
	}

	rootResource := "file-root:" + rootCertPath
	gotSecretRoot, err := sc.GenerateSecret(ctx, conID, rootResource, "jwtToken1")

	if err == nil {
		t.Fatalf("Expected to get error, but did not get")
	}
	if !strings.Contains(err.Error(), "no such file or directory") {
		t.Fatalf("Expected file not found error, but got %v", err)
	}
	if gotSecretRoot != nil {
		t.Fatalf("Expected to get nil secret but got %v", gotSecret)
	}
	length := 0
	sc.secrets.Range(func(k interface{}, v interface{}) bool {
		length++
		return true
	})
	if length > 0 {
		t.Fatalf("Expected zero secrets in cache, but got %v", length)
	}
}

func verifySecret(gotSecret *security.SecretItem, expectedSecret *security.SecretItem) error {
	if expectedSecret.ResourceName != gotSecret.ResourceName {
		return fmt.Errorf("resource name verification error: expected %s but got %s", expectedSecret.ResourceName,
			gotSecret.ResourceName)
	}
	if !bytes.Equal(expectedSecret.CertificateChain, gotSecret.CertificateChain) {
		return fmt.Errorf("cert chain verification error: expected %s but got %s", string(expectedSecret.CertificateChain),
			string(gotSecret.CertificateChain))
	}
	if !bytes.Equal(expectedSecret.PrivateKey, gotSecret.PrivateKey) {
		return fmt.Errorf("k8sKey verification error: expected %s but got %s", string(expectedSecret.PrivateKey),
			string(gotSecret.PrivateKey))
	}
	return nil
}

func verifyRootCASecret(gotSecret *security.SecretItem, expectedSecret *security.SecretItem) error {
	if expectedSecret.ResourceName != gotSecret.ResourceName {
		return fmt.Errorf("resource name verification error: expected %s but got %s", expectedSecret.ResourceName,
			gotSecret.ResourceName)
	}
	if !bytes.Equal(expectedSecret.RootCert, gotSecret.RootCert) {
		return fmt.Errorf("root cert verification error: expected %v but got %v", expectedSecret.RootCert,
			gotSecret.RootCert)
	}
	if expectedSecret.ExpireTime != gotSecret.ExpireTime {
		return fmt.Errorf("root cert expiration time verification error: expected %v but got %v",
			expectedSecret.ExpireTime, gotSecret.ExpireTime)
	}
	return nil
}

func TestShouldRotate(t *testing.T) {
	now := time.Now()

	testCases := map[string]struct {
		secret       *security.SecretItem
		sc           *SecretCache
		shouldRotate bool
	}{
		"Not in grace period": {
			secret: &security.SecretItem{
				ResourceName: "test1",
				ExpireTime:   now.Add(time.Hour),
				CreatedTime:  now.Add(-time.Hour),
			},
			sc:           &SecretCache{configOptions: &security.Options{SecretRotationGracePeriodRatio: 0.4}},
			shouldRotate: false,
		},
		"In grace period": {
			secret: &security.SecretItem{
				ResourceName: "test2",
				ExpireTime:   now.Add(time.Hour),
				CreatedTime:  now.Add(-time.Hour),
			},
			sc:           &SecretCache{configOptions: &security.Options{SecretRotationGracePeriodRatio: 0.6}},
			shouldRotate: true,
		},
		"Passed the expiration": {
			secret: &security.SecretItem{
				ResourceName: "test3",
				ExpireTime:   now.Add(-time.Minute),
				CreatedTime:  now.Add(-time.Hour),
			},
			sc:           &SecretCache{configOptions: &security.Options{SecretRotationGracePeriodRatio: 0}},
			shouldRotate: true,
		},
	}

	for name, tc := range testCases {
		if tc.sc.shouldRotate(tc.secret) != tc.shouldRotate {
			t.Errorf("%s: unexpected shouldRotate return. Expected: %v", name, tc.shouldRotate)
		}
	}
}
