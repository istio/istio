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

package cache

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"istio.io/istio/security/pkg/nodeagent/cache/mock"
	"istio.io/istio/security/pkg/nodeagent/plugin"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/istio/security/pkg/nodeagent/model"
	"istio.io/istio/security/pkg/nodeagent/secretfetcher"
	nodeagentutil "istio.io/istio/security/pkg/nodeagent/util"
)

var (
	certchain, _        = ioutil.ReadFile("./testdata/cert-chain.pem")
	mockCertChain1st    = []string{"foo", "rootcert"}
	mockCertChainRemain = []string{string(certchain)}
	testResourceName    = "default"

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
	// The cert chain in ./testdata/cert-chain.pem
	testDataCertChain = []byte(`-----BEGIN CERTIFICATE-----
MIIDnzCCAoegAwIBAgIJAON1ifrBZ2/BMA0GCSqGSIb3DQEBCwUAMIGLMQswCQYD
VQQGEwJVUzETMBEGA1UECAwKQ2FsaWZvcm5pYTESMBAGA1UEBwwJU3Vubnl2YWxl
MQ4wDAYDVQQKDAVJc3RpbzENMAsGA1UECwwEVGVzdDEQMA4GA1UEAwwHUm9vdCBD
QTEiMCAGCSqGSIb3DQEJARYTdGVzdHJvb3RjYUBpc3Rpby5pbzAgFw0xODAxMjQx
OTE1NTFaGA8yMTE3MTIzMTE5MTU1MVowWTELMAkGA1UEBhMCVVMxEzARBgNVBAgT
CkNhbGlmb3JuaWExEjAQBgNVBAcTCVN1bm55dmFsZTEOMAwGA1UEChMFSXN0aW8x
ETAPBgNVBAMTCElzdGlvIENBMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKC
AQEAyzCxr/xu0zy5rVBiso9ffgl00bRKvB/HF4AX9/ytmZ6Hqsy13XIQk8/u/By9
iCvVwXIMvyT0CbiJq/aPEj5mJUy0lzbrUs13oneXqrPXf7ir3HzdRw+SBhXlsh9z
APZJXcF93DJU3GabPKwBvGJ0IVMJPIFCuDIPwW4kFAI7R/8A5LSdPrFx6EyMXl7K
M8jekC0y9DnTj83/fY72WcWX7YTpgZeBHAeeQOPTZ2KYbFal2gLsar69PgFS0Tom
ESO9M14Yit7mzB1WDK2z9g3r+zLxENdJ5JG/ZskKe+TO4Diqi5OJt/h8yspS1ck8
LJtCole9919umByg5oruflqIlQIDAQABozUwMzALBgNVHQ8EBAMCAgQwDAYDVR0T
BAUwAwEB/zAWBgNVHREEDzANggtjYS5pc3Rpby5pbzANBgkqhkiG9w0BAQsFAAOC
AQEAltHEhhyAsve4K4bLgBXtHwWzo6SpFzdAfXpLShpOJNtQNERb3qg6iUGQdY+w
A2BpmSkKr3Rw/6ClP5+cCG7fGocPaZh+c+4Nxm9suMuZBZCtNOeYOMIfvCPcCS+8
PQ/0hC4/0J3WJKzGBssaaMufJxzgFPPtDJ998kY8rlROghdSaVt423/jXIAYnP3Y
05n8TGERBj7TLdtIVbtUIx3JHAo3PWJywA6mEDovFMJhJERp9sDHIr1BbhXK1TFN
Z6HNH6gInkSSMtvC4Ptejb749PTaePRPF7ID//eq/3AH8UK50F3TQcLjEqWUsJUn
aFKltOc+RAjzDklcUPeG4Y6eMA==
-----END CERTIFICATE-----`)
	testDataCertChainExpireTime, _ = nodeagentutil.ParseCertAndGetExpiryTimestamp(testDataCertChain)
)

func TestWorkloadAgentGenerateSecret(t *testing.T) {
	testWorkloadAgentGenerateSecret(t, false)
}

func TestWorkloadAgentGenerateSecretWithPluginProvider(t *testing.T) {
	testWorkloadAgentGenerateSecret(t, true)
}

func testWorkloadAgentGenerateSecret(t *testing.T, isUsingPluginProvider bool) {
	fakeCACli := mock.NewMockCAClient(mockCertChain1st, mockCertChainRemain, 0.1)
	opt := Options{
		SecretTTL:                time.Minute,
		RotationInterval:         300 * time.Microsecond,
		EvictionDuration:         60 * time.Second,
		InitialBackoffInMilliSec: 10,
		SkipValidateCert:         true,
	}

	if isUsingPluginProvider {
		fakePlugin := mock.NewMockTokenExchangeServer()
		opt.Plugins = []plugin.Plugin{fakePlugin}
	}

	fetcher := &secretfetcher.SecretFetcher{
		UseCaClient: true,
		CaClient:    fakeCACli,
	}
	sc := NewSecretCache(fetcher, notifyCb, opt)
	atomic.StoreUint32(&sc.skipTokenExpireCheck, 0)
	defer func() {
		sc.Close()
		atomic.StoreUint32(&sc.skipTokenExpireCheck, 1)
	}()

	checkBool(t, "opt.AlwaysValidTokenFlag default", opt.AlwaysValidTokenFlag, false)

	conID := "proxy1-id"
	ctx := context.Background()
	gotSecret, err := sc.GenerateSecret(ctx, conID, testResourceName, "jwtToken1")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}

	if got, want := gotSecret.CertificateChain, convertToBytes(mockCertChain1st); !bytes.Equal(got, want) {
		t.Errorf("CertificateChain: got: %v, want: %v", got, want)
	}

	checkBool(t, "SecretExist", sc.SecretExist(conID, testResourceName, "jwtToken1", gotSecret.Version), true)
	checkBool(t, "SecretExist", sc.SecretExist(conID, testResourceName, "nonexisttoken", gotSecret.Version), false)

	gotSecretRoot, err := sc.GenerateSecret(ctx, conID, RootCertReqResourceName, "jwtToken1")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	if got, want := gotSecretRoot.RootCert, []byte("rootcert"); !bytes.Equal(got, want) {
		t.Errorf("CertificateChain: got: %v, want: %v", got, want)
	}

	checkBool(t, "SecretExist", sc.SecretExist(conID, RootCertReqResourceName, "jwtToken1", gotSecretRoot.Version), true)
	checkBool(t, "SecretExist", sc.SecretExist(conID, RootCertReqResourceName, "nonexisttoken", gotSecretRoot.Version), false)

	if got, want := atomic.LoadUint64(&sc.rootCertChangedCount), uint64(0); got != want {
		t.Errorf("rootCertChangedCount: got: %v, want: %v", got, want)
	}

	key := ConnKey{
		ConnectionID: conID,
		ResourceName: testResourceName,
	}
	cachedSecret, found := sc.secrets.Load(key)
	if !found {
		t.Errorf("Failed to find secret for proxy %q from secret store: %v", conID, err)
	}
	if !reflect.DeepEqual(*gotSecret, cachedSecret) {
		t.Errorf("Secret key: got %+v, want %+v", *gotSecret, cachedSecret)
	}

	sc.configOptions.SkipValidateCert = false
	// Try to get secret again using different jwt token, verify secret is re-generated.
	gotSecret, err = sc.GenerateSecret(ctx, conID, testResourceName, "newToken")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	if got, want := gotSecret.CertificateChain, convertToBytes(mockCertChainRemain); !bytes.Equal(got, want) {
		t.Errorf("CertificateChain: got: %v, want: %v", got, want)
	}

	// Root cert is parsed from CSR response, it's updated since 2nd CSR is different from 1st.
	if got, want := atomic.LoadUint64(&sc.rootCertChangedCount), uint64(1); got != want {
		t.Errorf("rootCertChangedCount: got: %v, want: %v", got, want)
	}

	// Wait until unused secrets are evicted.
	wait := 500 * time.Millisecond
	retries := 0
	for ; retries < 3; retries++ {
		time.Sleep(wait)
		if _, found := sc.secrets.Load(conID); found {
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

func TestWorkloadAgentRefreshSecret(t *testing.T) {
	fakeCACli := mock.NewMockCAClient(mockCertChain1st, mockCertChainRemain, 0)
	opt := Options{
		SecretTTL:                200 * time.Microsecond,
		RotationInterval:         200 * time.Microsecond,
		EvictionDuration:         10 * time.Second,
		InitialBackoffInMilliSec: 10,
		SkipValidateCert:         true,
	}
	fetcher := &secretfetcher.SecretFetcher{
		UseCaClient: true,
		CaClient:    fakeCACli,
	}
	sc := NewSecretCache(fetcher, notifyCb, opt)
	atomic.StoreUint32(&sc.skipTokenExpireCheck, 0)
	defer func() {
		sc.Close()
		atomic.StoreUint32(&sc.skipTokenExpireCheck, 1)
	}()

	testConnID := "proxy1-id"
	_, err := sc.GenerateSecret(context.Background(), testConnID, testResourceName, "jwtToken1")
	if err != nil {
		t.Fatalf("Failed to get secrets for %q: %v", testConnID, err)
	}

	for i := 0; i < 10; i++ {
		id := "proxy-id" + strconv.Itoa(i)
		sc.GenerateSecret(context.Background(), id, testResourceName, "jwtToken1")
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
		ResourceName: testResourceName,
	}
	if _, found := sc.secrets.Load(key); !found {
		t.Errorf("Failed to find secret for %+v from cache", key)
	}

	sc.DeleteSecret(testConnID, testResourceName)
	if _, found := sc.secrets.Load(key); found {
		t.Errorf("Found deleted secret for %+v from cache", key)
	}
}

// TestGatewayAgentGenerateSecret verifies that ingress gateway agent manages secret cache correctly.
func TestGatewayAgentGenerateSecret(t *testing.T) {
	sc := createSecretCache()
	fetcher := sc.fetcher
	atomic.StoreUint32(&sc.skipTokenExpireCheck, 0)
	defer func() {
		sc.Close()
		atomic.StoreUint32(&sc.skipTokenExpireCheck, 1)
	}()

	connID1 := "proxy1-id"
	connID2 := "proxy2-id"
	ctx := context.Background()

	type expectedSecret struct {
		exist  bool
		secret *model.SecretItem
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
					secret: &model.SecretItem{
						ResourceName:     k8sGenericSecretName,
						CertificateChain: k8sCertChain,
						ExpireTime:       k8sCertChainExpireTime,
						PrivateKey:       k8sKey,
					},
				},
				{
					exist: true,
					secret: &model.SecretItem{
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
					secret: &model.SecretItem{
						ResourceName:     k8sTLSSecretName,
						CertificateChain: k8sCertChain,
						ExpireTime:       k8sCertChainExpireTime,
						PrivateKey:       k8sKey,
					},
				},
				{
					exist: false,
					secret: &model.SecretItem{
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
	atomic.StoreUint32(&sc.skipTokenExpireCheck, 0)
	defer func() {
		sc.Close()
		atomic.StoreUint32(&sc.skipTokenExpireCheck, 1)
	}()

	connID1 := "proxy1-id"
	connID2 := "proxy2-id"
	ctx := context.Background()

	type expectedSecret struct {
		exist  bool
		secret *model.SecretItem
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
				secret: &model.SecretItem{
					ResourceName:     k8sGenericSecretName,
					CertificateChain: k8sTLSFallbackSecretCertChain,
					ExpireTime:       k8sTLSFallbackSecretCertChainExpireTime,
					PrivateKey:       k8sTLSFallbackSecretKey,
				},
			},
			expectedSecrets: []expectedSecret{
				{
					exist: true,
					secret: &model.SecretItem{
						ResourceName:     k8sGenericSecretName,
						CertificateChain: k8sCertChain,
						ExpireTime:       k8sCertChainExpireTime,
						PrivateKey:       k8sKey,
					},
				},
				{
					exist: true,
					secret: &model.SecretItem{
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
				secret: &model.SecretItem{
					ResourceName:     k8sTLSSecretName,
					CertificateChain: k8sTLSFallbackSecretCertChain,
					ExpireTime:       k8sTLSFallbackSecretCertChainExpireTime,
					PrivateKey:       k8sTLSFallbackSecretKey,
				},
			},
			expectedSecrets: []expectedSecret{
				{
					exist: true,
					secret: &model.SecretItem{
						ResourceName:     k8sTLSSecretName,
						CertificateChain: k8sCertChain,
						ExpireTime:       k8sCertChainExpireTime,
						PrivateKey:       k8sKey,
					},
				},
				{
					exist: false,
					secret: &model.SecretItem{
						ResourceName: k8sTLSSecretName + "-cacert",
					},
				},
			},
		},
	}

	fetcher.AddSecret(k8sTestTLSFallbackSecret)
	for _, c := range cases {
		if sc.ShouldWaitForIngressGatewaySecret(c.connID, c.expectedFbSecret.secret.ResourceName, "") {
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
		// When secret is deleted, node agent should not wait for ingress gateway secret.
		fetcher.DeleteSecret(c.addSecret)
		if sc.ShouldWaitForIngressGatewaySecret(c.connID, c.expectedFbSecret.secret.ResourceName, "") {
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
	opt := Options{
		SecretTTL:                time.Minute,
		RotationInterval:         300 * time.Microsecond,
		EvictionDuration:         2 * time.Second,
		InitialBackoffInMilliSec: 10,
		SkipValidateCert:         true,
	}
	return NewSecretCache(fetcher, notifyCb, opt)
}

// TestGatewayAgentDeleteSecret verifies that ingress gateway agent deletes secret cache correctly.
func TestGatewayAgentDeleteSecret(t *testing.T) {
	sc := createSecretCache()
	fetcher := sc.fetcher
	atomic.StoreUint32(&sc.skipTokenExpireCheck, 0)
	defer func() {
		sc.Close()
		atomic.StoreUint32(&sc.skipTokenExpireCheck, 1)
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
	atomic.StoreUint32(&sc.skipTokenExpireCheck, 0)
	defer func() {
		sc.Close()
		atomic.StoreUint32(&sc.skipTokenExpireCheck, 1)
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
	newK8sTestSecret := model.SecretItem{
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

func TestSetAlwaysValidTokenFlag(t *testing.T) {
	fakeCACli := mock.NewMockCAClient(mockCertChain1st, mockCertChainRemain, 0)
	opt := Options{
		SecretTTL:                200 * time.Microsecond,
		RotationInterval:         200 * time.Microsecond,
		EvictionDuration:         10 * time.Second,
		InitialBackoffInMilliSec: 10,
		AlwaysValidTokenFlag:     true,
		SkipValidateCert:         true,
	}
	fetcher := &secretfetcher.SecretFetcher{
		UseCaClient: true,
		CaClient:    fakeCACli,
	}
	sc := NewSecretCache(fetcher, notifyCb, opt)
	defer func() {
		sc.Close()
	}()

	checkBool(t, "isTokenExpired", sc.isTokenExpired(), false)
	_, err := sc.GenerateSecret(context.Background(), "proxy1-id", testResourceName, "jwtToken1")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
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
	for _, tc := range testCases {
		ret := sc.keyCertificateExist(tc.certPath, tc.keyPath)
		if tc.expectResult != ret {
			t.Errorf("unexpected result is returned!")
		}
	}
}

func TestGenerateRootCertFromExistingFile(t *testing.T) {
	sc := createSecretCache()
	atomic.StoreUint32(&sc.skipTokenExpireCheck, 0)
	defer func() {
		sc.Close()
		atomic.StoreUint32(&sc.skipTokenExpireCheck, 1)
	}()

	connID1 := "proxy1-id"
	cases := []struct {
		certPath        string
		connID          string
		expectedSecrets *model.SecretItem
	}{
		{
			certPath: "./testdata/cert-chain.pem",
			connID:   connID1,
			expectedSecrets: &model.SecretItem{
				ResourceName: RootCertReqResourceName,
				RootCert:     testDataCertChain,
				ExpireTime:   testDataCertChainExpireTime,
			},
		},
	}

	for _, c := range cases {
		es := c.expectedSecrets
		key := ConnKey{
			ConnectionID: c.connID,
			ResourceName: es.ResourceName,
		}
		gotSecret, err := sc.generateRootCertFromExistingFile("./testdata/cert-chain.pem",
			"token", key)
		if err != nil {
			t.Fatalf("Failed to get secrets: %v", err)
		}
		if err := verifyRootCASecret(gotSecret, es); err != nil {
			t.Errorf("Secret verification failed: %v", err)
		}
	}
}

func TestGenerateKeyCertFromExistingFiles(t *testing.T) {
	sc := createSecretCache()
	atomic.StoreUint32(&sc.skipTokenExpireCheck, 0)
	defer func() {
		sc.Close()
		atomic.StoreUint32(&sc.skipTokenExpireCheck, 1)
	}()

	connID1 := "proxy1-id"
	cases := []struct {
		certPath        string
		connID          string
		expectedSecrets *model.SecretItem
	}{
		{
			certPath: "./testdata/cert-chain.pem",
			connID:   connID1,
			expectedSecrets: &model.SecretItem{
				ResourceName:     WorkloadKeyCertResourceName,
				CertificateChain: testDataCertChain,
				ExpireTime:       testDataCertChainExpireTime,
				PrivateKey:       testDataCertChain,
			},
		},
	}

	for _, c := range cases {
		es := c.expectedSecrets
		key := ConnKey{
			ConnectionID: c.connID,
			ResourceName: es.ResourceName,
		}
		gotSecret, err := sc.generateKeyCertFromExistingFiles("./testdata/cert-chain.pem",
			"./testdata/cert-chain.pem", "token", key)
		if err != nil {
			t.Fatalf("Failed to get secrets: %v", err)
		}
		if err := verifySecret(gotSecret, es); err != nil {
			t.Errorf("Secret verification failed: %v", err)
		}
	}
}

func verifySecret(gotSecret *model.SecretItem, expectedSecret *model.SecretItem) error {
	if expectedSecret.ResourceName != gotSecret.ResourceName {
		return fmt.Errorf("resource name verification error: expected %s but got %s", expectedSecret.ResourceName,
			gotSecret.ResourceName)
	}
	if !bytes.Equal(expectedSecret.CertificateChain, gotSecret.CertificateChain) {
		return fmt.Errorf("cert chain verification error: expected %v but got %v", expectedSecret.CertificateChain,
			gotSecret.CertificateChain)
	}
	if !bytes.Equal(expectedSecret.PrivateKey, gotSecret.PrivateKey) {
		return fmt.Errorf("k8sKey verification error: expected %v but got %v", expectedSecret.PrivateKey,
			gotSecret.PrivateKey)
	}
	return nil
}

func verifyRootCASecret(gotSecret *model.SecretItem, expectedSecret *model.SecretItem) error {
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

func notifyCb(_ ConnKey, _ *model.SecretItem) error {
	return nil
}

func convertToBytes(ss []string) []byte {
	res := []byte{}
	for _, s := range ss {
		res = append(res, []byte(s)...)
	}
	return res
}

func TestWorkloadAgentGenerateSecretFromFile(t *testing.T) {
	testWorkloadAgentGenerateSecretFromFile(t)
}

func testWorkloadAgentGenerateSecretFromFile(t *testing.T) {
	fakeCACli := mock.NewMockCAClient(mockCertChain1st, mockCertChainRemain, 0.1)
	opt := Options{
		SecretTTL:                time.Minute,
		RotationInterval:         300 * time.Microsecond,
		EvictionDuration:         60 * time.Second,
		InitialBackoffInMilliSec: 10,
		SkipValidateCert:         true,
	}

	existingCertChainFile = "./testdata/cert-chain.pem"
	existingKeyFile = "./testdata/privatekey.pem"
	ExistingRootCertFile = "./testdata/cert-chain.pem"

	fetcher := &secretfetcher.SecretFetcher{
		UseCaClient: true,
		CaClient:    fakeCACli,
	}
	sc := NewSecretCache(fetcher, notifyCb, opt)
	atomic.StoreUint32(&sc.skipTokenExpireCheck, 0)
	defer func() {
		sc.Close()
		atomic.StoreUint32(&sc.skipTokenExpireCheck, 1)
	}()

	conID := "proxy1-id"
	ctx := context.Background()
	gotSecret, err := sc.GenerateSecret(ctx, conID, testResourceName, "jwtToken1")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}

	if got, want := gotSecret.CertificateChain, certchain; !bytes.Equal(got, want) {
		t.Errorf("CertificateChain: got: %v, want: %v", got, want)
	}
	privateKey, _ := ioutil.ReadFile(existingKeyFile)
	if got, want := gotSecret.PrivateKey, privateKey; !bytes.Equal(got, want) {
		t.Errorf("PrivateKey: got: %v, want: %v", got, want)
	}

	checkBool(t, "SecretExist", sc.SecretExist(conID, testResourceName, "jwtToken1", gotSecret.Version), true)

	gotSecretRoot, err := sc.GenerateSecret(ctx, conID, RootCertReqResourceName, "jwtToken1")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	if got, want := gotSecretRoot.RootCert, certchain; !bytes.Equal(got, want) {
		t.Errorf("CertificateChain: got: %v, want: %v", got, want)
	}

	checkBool(t, "SecretExist", sc.SecretExist(conID, RootCertReqResourceName, "jwtToken1", gotSecretRoot.Version), true)

	if got, want := atomic.LoadUint64(&sc.rootCertChangedCount), uint64(0); got != want {
		t.Errorf("rootCertChangedCount: got: %v, want: %v", got, want)
	}

	key := ConnKey{
		ConnectionID: conID,
		ResourceName: testResourceName,
	}
	cachedSecret, found := sc.secrets.Load(key)
	if !found {
		t.Errorf("Failed to find secret for proxy %q from secret store: %v", conID, err)
	}
	if !reflect.DeepEqual(*gotSecret, cachedSecret) {
		t.Errorf("Secret key: got %+v, want %+v", *gotSecret, cachedSecret)
	}

	// Wait until unused secrets are evicted.
	wait := 500 * time.Millisecond
	retries := 0
	for ; retries < 3; retries++ {
		time.Sleep(wait)
		if _, found := sc.secrets.Load(conID); found {
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
