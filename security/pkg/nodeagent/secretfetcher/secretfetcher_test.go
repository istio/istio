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

package secretfetcher

import (
	"bytes"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/istio/pkg/security"

	nodeagentutil "istio.io/istio/security/pkg/nodeagent/util"
)

var (
	k8sTestCertChainA = []byte(`-----BEGIN CERTIFICATE-----
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
	k8sTestCertChainExpireTimeA, _ = nodeagentutil.ParseCertAndGetExpiryTimestamp(k8sTestCertChainA)
	k8sTestCaCertA                 = []byte(`-----BEGIN CERTIFICATE-----
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
	k8sTestCaCertExpireTimeA, _ = nodeagentutil.ParseCertAndGetExpiryTimestamp(k8sTestCaCertA)
	k8sTestCertChainB           = []byte(`-----BEGIN CERTIFICATE-----
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
	k8sTestCaCertB = []byte(`-----BEGIN CERTIFICATE-----
MIIFgTCCA2mgAwIBAgIDEAISMA0GCSqGSIb3DQEBCwUAMFoxCzAJBgNVBAYTAlVT
MQ8wDQYDVQQIDAZEZW5pYWwxFDASBgNVBAcMC1NwcmluZ2ZpZWxkMQwwCgYDVQQK
DANEaXMxFjAUBgNVBAMMDSouZXhhbXBsZS5jb20wHhcNMTkwNTI5MjMxNDM5WhcN
MzkwNTI0MjMxNDM5WjBEMQswCQYDVQQGEwJVUzEPMA0GA1UECAwGRGVuaWFsMQww
CgYDVQQKDANEaXMxFjAUBgNVBAMMDSouZXhhbXBsZS5jb20wggIiMA0GCSqGSIb3
DQEBAQUAA4ICDwAwggIKAoICAQDIFHI/+8ZhJJPuktwXLG9ejV8R3MRS7+0KvJut
SEZASZbE+x4/bvrxy7Y3ZmoY8W7r/HxAqj9xmvfmqo355Ix4PqK2S/v2jiVpLf5o
DTEtOI63SSLUvrOdmhjnQz+ZMqLZCWCIQcCXSCfhBWWRyH+iOpAcaLOOaoZI/qCR
FCqOCmxh3k9Kthex7gwTNCqaZ+jrhq603HrfZ7DtRolAX7oz4ucDC9qraVvGx6MO
nNh5E0qvOHhuZ1sExWyG9NqEkPaqfDeK0svsiHPWdm6jvo0r/8DiDKeda+MvM1C/
ZQxB+paH0qmTqxnsA3AngP1w3NzGvHxCXFwnV3D5iMTayohpc0dyZuaGUMKWKj5R
jq0LwteB1uAyyhEz2SG8qJL+QbawM1qxK1EjJOFvk/pdTnN7HzDc/+bklUnYBbbd
vdaF+JAdYpDEZlMrHTVfeY7kMM9TbHQbmsmNoUr/GHzDCqoiEa22DITJwSRf2rUe
VISsvGCyXBSyUilK5dUWmFl7AXprpMBIKQL9X50Ssfx3rs2mWGEP0YySFRn0zrxl
wg1yBygh8HC+HGNaMdXZDGXeSJzITQI1anngHhkOaOh29UEGM2k5SNiWOZxQEV4J
L5BBjaJBCifAZcCZd3sPkxmVurZi9vAVc9JT+Avkg4UuDu/HWYpeE302S3l5QSKI
eVeNEwIDAQABo2YwZDAdBgNVHQ4EFgQURfQW0owHXQLtnkMvKDSxCY4Wy5owHwYD
VR0jBBgwFoAUd3dG9VBzLMLYT6z3+LT/U3p1SpswEgYDVR0TAQH/BAgwBgEB/wIB
ADAOBgNVHQ8BAf8EBAMCAYYwDQYJKoZIhvcNAQELBQADggIBAIUilHtj2m5qV+xc
ElVY3Gsv0mhxHNbEkG+EgOoWd1QWjvz58gkddgGxAb5mJYM5FSdw4yoSIuUYy6F0
dXxAElWFplNiPY46OJ2/MwINGNZWfkI7jxVCYanHXGJa5llVpPNhLpJEVGt8FOkd
nLu2ZKCLLSAOC9Z3R/FNxF4HiAuN8Z3OYX7jtaUSdgPogsjzsuWWxuy5Rs8A8hM4
483UfakoHjLtXAzbdQ1sA7k8YvY3u1t2b3x9jDEcHmz2FYX+N0BtzgSsZvLxrT9b
wUC+g4Lspl/Lnp8Jrg8k/DQdcC1g0rVMi5nLNtHbH/2j502Na/TDtPANfAlf9gcd
9TjLsxqax8L7vXvrrcvnJTDZZVjA3NQhDV5EFuwjcSoUq02p7c63FYChNNCdEjKs
8MA+jH97xafhR6TvxW9R9BTcIwrrJmgmQ+b2hz9sqlE1ZDT4Biy4bUQiAvRTuSxX
ch+jNPLD3kyfDEixhE1+5luutC77b98qc3KWsG6l3XDQQf/YZ6h6iIkZsXorGtxg
sLcSOZBc3XyP5twMeOw2ZOMC0qLupFL2MBEmKerlHo5ehQpW16KBHWn1HxFL8j24
PAsalRNQlxxWYCEYsf60TIUSqtyt1P5G7S40Rn3CP9SnoX6Q3E0POxEGFe3SStAY
oCvHkuhGyVKRT4Ddff4gfbvMPlls
-----END CERTIFICATE-----`)
	k8sTestCaCertExpireTimeB, _ = nodeagentutil.ParseCertAndGetExpiryTimestamp(k8sTestCaCertB)
	k8sKeyA                     = []byte("fake private k8sKeyA")
	k8sCertChainA               = k8sTestCertChainA
	k8sCaCertA                  = k8sTestCaCertA
	k8sSecretNameA              = "test-scrtA"
	k8sTestGenericSecretA       = &v1.Secret{
		Data: map[string][]byte{
			genericScrtCert:   k8sCertChainA,
			genericScrtKey:    k8sKeyA,
			genericScrtCaCert: k8sCaCertA,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sSecretNameA,
			Namespace: "test-namespace",
		},
		Type: "test-secret",
	}
	k8sInvalidTestGenericSecretA = &v1.Secret{
		Data: map[string][]byte{
			genericScrtKey:    k8sKeyA,
			genericScrtCaCert: k8sCaCertA,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sSecretNameA,
			Namespace: "test-namespace",
		},
		Type: "test-secret",
	}

	k8sKeyB               = []byte("k8sKeyB private fake")
	k8sCertChainB         = k8sTestCertChainA
	k8sCaCertB            = k8sTestCaCertA
	k8sTestGenericSecretB = &v1.Secret{
		Data: map[string][]byte{
			genericScrtCert:   k8sCertChainB,
			genericScrtKey:    k8sKeyB,
			genericScrtCaCert: k8sCaCertB,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sSecretNameA,
			Namespace: "test-namespace",
		},
		Type: "test-secret",
	}

	k8sKeyC           = []byte("fake private k8sKeyC")
	k8sCertChainC     = k8sTestCertChainA
	k8sSecretNameC    = "test-scrtC"
	k8sTestTLSSecretC = &v1.Secret{
		Data: map[string][]byte{
			tlsScrtCert: k8sCertChainC,
			tlsScrtKey:  k8sKeyC,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sSecretNameC,
			Namespace: "test-namespace",
		},
		Type: "test-tls-secret",
	}

	k8sKeyD           = []byte("fake private k8sKeyD")
	k8sCertChainD     = k8sTestCertChainA
	k8sTestTLSSecretD = &v1.Secret{
		Data: map[string][]byte{
			tlsScrtCert: k8sCertChainD,
			tlsScrtKey:  k8sKeyD,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sSecretNameC,
			Namespace: "test-namespace",
		},
		Type: "test-tls-secret",
	}

	k8sSecretFallbackScrt    = "fallback-scrt"
	k8sFallbackKey           = []byte("fallback fake private key")
	k8sFallbackCertChain     = k8sTestCertChainB
	k8sTestTLSFallbackSecret = &v1.Secret{
		Data: map[string][]byte{
			tlsScrtCert: k8sFallbackCertChain,
			tlsScrtKey:  k8sFallbackKey,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sSecretFallbackScrt,
			Namespace: "test-namespace",
		},
		Type: "test-tls-secret",
	}

	k8sCASecretNameE        = "test-scrtE-cacert"
	k8sCaCertE              = k8sTestCaCertA
	k8sTestGenericCASecretE = &v1.Secret{
		Data: map[string][]byte{
			genericScrtCaCert: k8sCaCertE,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sCASecretNameE,
			Namespace: "test-namespace",
		},
		Type: "test-ca-secret",
	}

	k8sCaCertF       = k8sTestCaCertB
	k8sTestCASecretF = &v1.Secret{
		Data: map[string][]byte{
			tlsScrtCert: k8sCaCertF,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sCASecretNameE,
			Namespace: "test-namespace",
		},
		Type: "test-tls-ca-secret",
	}

	k8sSecretNameG           = "test-scrtG"
	k8sTestKubernetesSecretG = &v1.Secret{
		Data: map[string][]byte{
			tlsScrtCert:   k8sCertChainB,
			tlsScrtKey:    k8sKeyB,
			tlsScrtCaCert: k8sCaCertB,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sSecretNameG,
			Namespace: "test-namespace",
		},
		Type: "test-secret",
	}
)

type expectedSecret struct {
	exist  bool
	secret *security.SecretItem
}

// TestSecretFetcher verifies that secret fetcher is able to add kubernetes secret into local store,
// find secret by name, and delete secret by name.
func TestSecretFetcher(t *testing.T) {
	gSecretFetcher := &SecretFetcher{
		UseCaClient: false,
		DeleteCache: func(secretName string) {},
		UpdateCache: func(secretName string, ns security.SecretItem) {},
		// Set fallback secret name but no such secret is created.
		FallbackSecretName: "gateway-fallback",
	}
	gSecretFetcher.InitWithKubeClient(fake.NewSimpleClientset().CoreV1())
	if gSecretFetcher.UseCaClient {
		t.Error("secretFetcher should not use ca client")
	}
	ch := make(chan struct{})
	gSecretFetcher.Run(ch)

	// Searching a non-existing secret should return false.
	if _, ok := gSecretFetcher.FindGatewaySecret("non-existing-secret"); ok {
		t.Error("secretFetcher returns a secret non-existing-secret that should not exist")
	}

	// Add test secret and verify that key/cert pair is stored.
	expectedAddedSecrets := []expectedSecret{
		{
			exist: true,
			secret: &security.SecretItem{
				ResourceName:     k8sSecretNameA,
				CertificateChain: k8sCertChainA,
				ExpireTime:       k8sTestCertChainExpireTimeA,
				PrivateKey:       k8sKeyA,
			},
		},
		{
			exist: true,
			secret: &security.SecretItem{
				ResourceName: k8sSecretNameA + GatewaySdsCaSuffix,
				RootCert:     k8sCaCertA,
				ExpireTime:   k8sTestCaCertExpireTimeA,
			},
		},
	}
	var secretVersionOne string
	testAddSecret(t, gSecretFetcher, k8sTestGenericSecretA, expectedAddedSecrets, &secretVersionOne)

	// Delete test secret and verify that key/cert pair in secret is removed from local store.
	expectedDeletedSecrets := []expectedSecret{
		{
			exist:  false,
			secret: &security.SecretItem{ResourceName: k8sSecretNameA},
		},
		{
			exist:  false,
			secret: &security.SecretItem{ResourceName: k8sSecretNameA + GatewaySdsCaSuffix},
		},
	}
	testDeleteSecret(t, gSecretFetcher, k8sTestGenericSecretA, expectedDeletedSecrets)

	// Add test secret again and verify that key/cert pair is stored and version number is different.
	var secretVersionTwo string
	testAddSecret(t, gSecretFetcher, k8sTestGenericSecretA, expectedAddedSecrets, &secretVersionTwo)
	if secretVersionTwo == secretVersionOne {
		t.Errorf("added secret should have different version")
	}

	// Update test secret and verify that key/cert pair is changed and version number is different.
	expectedUpdateSecrets := []expectedSecret{
		{
			exist: true,
			secret: &security.SecretItem{
				ResourceName:     k8sSecretNameA,
				CertificateChain: k8sCertChainB,
				ExpireTime:       k8sTestCertChainExpireTimeA,
				PrivateKey:       k8sKeyB,
			},
		},
		{
			exist: true,
			secret: &security.SecretItem{
				ResourceName: k8sSecretNameA + GatewaySdsCaSuffix,
				RootCert:     k8sCaCertB,
				ExpireTime:   k8sTestCaCertExpireTimeA,
			},
		},
	}
	var secretVersionThree string
	testUpdateSecret(t, gSecretFetcher, k8sTestGenericSecretA, k8sTestGenericSecretB, expectedUpdateSecrets, &secretVersionThree)
	if secretVersionThree == secretVersionTwo || secretVersionThree == secretVersionOne {
		t.Errorf("updated secret should have different version")
	}

	// Add test ca only secret and verify that cacert is stored.
	expectedAddedCASecrets := []expectedSecret{
		{
			exist: true,
			secret: &security.SecretItem{
				ResourceName: k8sCASecretNameE,
				RootCert:     k8sCaCertE,
				ExpireTime:   k8sTestCaCertExpireTimeA,
			},
		},
	}
	var secretVersionFour string
	testAddSecret(t, gSecretFetcher, k8sTestGenericCASecretE, expectedAddedCASecrets, &secretVersionFour)

	// Update test ca only secret and verify that cacert is stored and version number is different.
	expectedUpdateCASecrets := []expectedSecret{
		{
			exist: true,
			secret: &security.SecretItem{
				ResourceName: k8sCASecretNameE,
				RootCert:     k8sCaCertF,
				ExpireTime:   k8sTestCaCertExpireTimeB,
			},
		},
	}
	var secretVersionFive string
	testUpdateSecret(t, gSecretFetcher, k8sTestGenericCASecretE, k8sTestCASecretF, expectedUpdateCASecrets, &secretVersionFive)
	if secretVersionFive == secretVersionFour {
		t.Errorf("updated secret should have different version")
	}

	// Delete test ca secret and verify that its cacert is removed from local store.
	expectedDeletedCASecrets := []expectedSecret{
		{
			exist:  false,
			secret: &security.SecretItem{ResourceName: k8sCASecretNameE},
		},
	}
	testDeleteSecret(t, gSecretFetcher, k8sTestGenericCASecretE, expectedDeletedCASecrets)

	// Add test secret and verify that key/cert pair is stored.
	expectedAddedSecrets = []expectedSecret{
		{
			exist: true,
			secret: &security.SecretItem{
				ResourceName:     k8sSecretNameG,
				CertificateChain: k8sCertChainB,
				ExpireTime:       k8sTestCertChainExpireTimeA,
				PrivateKey:       k8sKeyB,
			},
		},
		{
			exist: true,
			secret: &security.SecretItem{
				ResourceName: k8sSecretNameG + GatewaySdsCaSuffix,
				RootCert:     k8sCaCertB,
				ExpireTime:   k8sTestCaCertExpireTimeA,
			},
		},
	}
	testAddSecret(t, gSecretFetcher, k8sTestKubernetesSecretG, expectedAddedSecrets, &secretVersionOne)

	// Delete test secret and verify that key/cert pair in secret is removed from local store.
	expectedDeletedSecrets = []expectedSecret{
		{
			exist:  false,
			secret: &security.SecretItem{ResourceName: k8sSecretNameG},
		},
		{
			exist:  false,
			secret: &security.SecretItem{ResourceName: k8sSecretNameG + GatewaySdsCaSuffix},
		},
	}
	testDeleteSecret(t, gSecretFetcher, k8sTestKubernetesSecretG, expectedDeletedSecrets)

}

// TestSecretFetcherInvalidSecret verifies that if a secret does not have key or cert, secret fetcher
// will skip adding or updating with the invalid secret.
func TestSecretFetcherInvalidSecret(t *testing.T) {
	gSecretFetcher := &SecretFetcher{
		UseCaClient: false,
		DeleteCache: func(secretName string) {},
		UpdateCache: func(secretName string, ns security.SecretItem) {},
	}
	gSecretFetcher.InitWithKubeClient(fake.NewSimpleClientset().CoreV1())
	if gSecretFetcher.UseCaClient {
		t.Error("secretFetcher should not use ca client")
	}
	ch := make(chan struct{})
	gSecretFetcher.Run(ch)

	gSecretFetcher.scrtAdded(k8sInvalidTestGenericSecretA)
	if _, ok := gSecretFetcher.FindGatewaySecret(k8sInvalidTestGenericSecretA.GetName()); ok {
		t.Errorf("invalid secret should not be added into secret fetcher.")
	}

	var secretVersionOne string
	// Add test secret and verify that key/cert pair is stored.
	expectedAddedSecrets := []expectedSecret{
		{
			exist: true,
			secret: &security.SecretItem{
				ResourceName:     k8sSecretNameA,
				CertificateChain: k8sCertChainB,
				ExpireTime:       k8sTestCertChainExpireTimeA,
				PrivateKey:       k8sKeyB,
			},
		},
		{
			exist: true,
			secret: &security.SecretItem{
				ResourceName: k8sSecretNameA + GatewaySdsCaSuffix,
				RootCert:     k8sCaCertB,
				ExpireTime:   k8sTestCaCertExpireTimeA,
			},
		},
	}
	testAddSecret(t, gSecretFetcher, k8sTestGenericSecretB, expectedAddedSecrets, &secretVersionOne)
	// Try to update with an invalid secret, and verify that the invalid secret is not added.
	// Secret fetcher still owns old secret k8sTestGenericSecretB.
	gSecretFetcher.scrtUpdated(k8sTestGenericSecretB, k8sInvalidTestGenericSecretA)
	secret, ok := gSecretFetcher.FindGatewaySecret(k8sSecretNameA)
	if !ok {
		t.Errorf("secretFetcher failed to find secret %s", k8sSecretNameA)
	}
	if secretVersionOne != secret.Version {
		t.Errorf("version number does not match.")
	}
	if !bytes.Equal(k8sCertChainB, secret.CertificateChain) {
		t.Errorf("cert chain verification error: expected %v but got %v", k8sCertChainB, secret.CertificateChain)
	}
	if !bytes.Equal(k8sKeyB, secret.PrivateKey) {
		t.Errorf("private key verification error: expected %v but got %v", k8sKeyB, secret.PrivateKey)
	}
	casecret, ok := gSecretFetcher.FindGatewaySecret(k8sSecretNameA + GatewaySdsCaSuffix)
	if !ok || !bytes.Equal(k8sCaCertB, casecret.RootCert) {
		t.Errorf("root cert verification error: expected %v but got %v", k8sCaCertB, secret.RootCert)
	}
}

// TestSecretFetcherSkipSecret verifies that secret fetcher skips adding secrets if that secret
// is not a gateway secret.
func TestSecretFetcherSkipSecret(t *testing.T) {
	gSecretFetcher := &SecretFetcher{
		UseCaClient: false,
		DeleteCache: func(secretName string) {},
		UpdateCache: func(secretName string, ns security.SecretItem) {},
	}
	gSecretFetcher.InitWithKubeClient(fake.NewSimpleClientset().CoreV1())
	if gSecretFetcher.UseCaClient {
		t.Error("secretFetcher should not use ca client")
	}
	ch := make(chan struct{})
	gSecretFetcher.Run(ch)

	istioPrefixSecret := &v1.Secret{
		Data: map[string][]byte{
			genericScrtCert:   k8sCertChainA,
			genericScrtKey:    k8sKeyA,
			genericScrtCaCert: k8sCaCertA,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istioSecret",
			Namespace: "test-namespace",
		},
		Type: "test-secret",
	}

	gSecretFetcher.scrtAdded(istioPrefixSecret)
	if _, ok := gSecretFetcher.FindGatewaySecret(istioPrefixSecret.GetName()); ok {
		t.Errorf("istio secret should not be added into secret fetcher.")
	}

	prometheusPrefixSecret := &v1.Secret{
		Data: map[string][]byte{
			genericScrtCert:   k8sCertChainA,
			genericScrtKey:    k8sKeyA,
			genericScrtCaCert: k8sCaCertA,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prometheusSecret",
			Namespace: "test-namespace",
		},
		Type: "test-secret",
	}

	gSecretFetcher.scrtAdded(prometheusPrefixSecret)
	if _, ok := gSecretFetcher.FindGatewaySecret(prometheusPrefixSecret.GetName()); ok {
		t.Errorf("prometheus secret should not be added into secret fetcher.")
	}

	var secretVersionOne string
	// Add test secret and verify that key/cert pair is stored.
	expectedAddedSecrets := []expectedSecret{
		{
			exist: true,
			secret: &security.SecretItem{
				ResourceName:     k8sSecretNameA,
				CertificateChain: k8sCertChainB,
				ExpireTime:       k8sTestCertChainExpireTimeA,
				PrivateKey:       k8sKeyB,
			},
		},
		{
			exist: true,
			secret: &security.SecretItem{
				ResourceName: k8sSecretNameA + GatewaySdsCaSuffix,
				RootCert:     k8sCaCertB,
				ExpireTime:   k8sTestCaCertExpireTimeA,
			},
		},
	}
	testAddSecret(t, gSecretFetcher, k8sTestGenericSecretB, expectedAddedSecrets, &secretVersionOne)
	// Try to update with an invalid secret, and verify that the invalid secret is not added.
	// Secret fetcher still owns old secret k8sTestGenericSecretB.
	tokenSecretB := &v1.Secret{
		Data: map[string][]byte{
			genericScrtCert:   k8sCertChainB,
			genericScrtKey:    k8sKeyB,
			genericScrtCaCert: k8sCaCertB,
			scrtTokenField:    []byte("fake token"),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sSecretNameA,
			Namespace: "test-namespace",
		},
		Type: "test-secret",
	}
	gSecretFetcher.scrtUpdated(k8sTestGenericSecretB, tokenSecretB)
	secret, ok := gSecretFetcher.FindGatewaySecret(k8sSecretNameA)
	if !ok {
		t.Errorf("secretFetcher failed to find secret %s", k8sSecretNameA)
	}
	if secretVersionOne != secret.Version {
		t.Errorf("version number does not match.")
	}
	if !bytes.Equal(k8sCertChainB, secret.CertificateChain) {
		t.Errorf("cert chain verification error: expected %v but got %v", k8sCertChainB, secret.CertificateChain)
	}
	if !bytes.Equal(k8sKeyB, secret.PrivateKey) {
		t.Errorf("private key verification error: expected %v but got %v", k8sKeyB, secret.PrivateKey)
	}
	casecret, ok := gSecretFetcher.FindGatewaySecret(k8sSecretNameA + GatewaySdsCaSuffix)
	if !ok || !bytes.Equal(k8sCaCertB, casecret.RootCert) {
		t.Errorf("root cert verification error: expected %v but got %v", k8sCaCertB, secret.RootCert)
	}
}

// TestSecretFetcherTlsSecretFormat verifies that secret fetcher is able to extract key/cert
// from TLS secret format.
func TestSecretFetcherTlsSecretFormat(t *testing.T) {
	gSecretFetcher := &SecretFetcher{
		UseCaClient: false,
		DeleteCache: func(secretName string) {},
		UpdateCache: func(secretName string, ns security.SecretItem) {},
	}
	gSecretFetcher.InitWithKubeClient(fake.NewSimpleClientset().CoreV1())
	if gSecretFetcher.UseCaClient {
		t.Error("secretFetcher should not use ca client")
	}
	ch := make(chan struct{})
	gSecretFetcher.Run(ch)

	// Searching a non-existing secret should return false.
	if _, ok := gSecretFetcher.FindGatewaySecret("non-existing-secret"); ok {
		t.Error("secretFetcher returns a secret non-existing-secret that should not exist")
	}

	// Add test secret and verify that key/cert pair is stored.
	expectedAddedSecrets := []expectedSecret{
		{
			exist: true,
			secret: &security.SecretItem{
				ResourceName:     k8sSecretNameC,
				CertificateChain: k8sCertChainC,
				ExpireTime:       k8sTestCertChainExpireTimeA,
				PrivateKey:       k8sKeyC,
			},
		},
		{
			exist: false,
			secret: &security.SecretItem{
				ResourceName: k8sSecretNameC + GatewaySdsCaSuffix,
			},
		},
	}
	var secretVersion string
	testAddSecret(t, gSecretFetcher, k8sTestTLSSecretC, expectedAddedSecrets, &secretVersion)

	// Delete test secret and verify that key/cert pair in secret is removed from local store.
	expectedDeletedSecret := []expectedSecret{
		{
			exist:  false,
			secret: &security.SecretItem{ResourceName: k8sSecretNameC},
		},
		{
			exist:  false,
			secret: &security.SecretItem{ResourceName: k8sSecretNameC + GatewaySdsCaSuffix},
		},
	}
	testDeleteSecret(t, gSecretFetcher, k8sTestTLSSecretC, expectedDeletedSecret)

	// Add test secret again and verify that key/cert pair is stored and version number is different.
	testAddSecret(t, gSecretFetcher, k8sTestTLSSecretC, expectedAddedSecrets, &secretVersion)

	// Update test secret and verify that key/cert pair is changed and version number is different.
	expectedUpdateSecret := []expectedSecret{
		{
			exist: true,
			secret: &security.SecretItem{
				ResourceName:     k8sSecretNameC,
				CertificateChain: k8sCertChainD,
				ExpireTime:       k8sTestCertChainExpireTimeA,
				PrivateKey:       k8sKeyD,
			},
		},
		{
			exist:  false,
			secret: &security.SecretItem{ResourceName: k8sSecretNameC + GatewaySdsCaSuffix},
		},
	}
	var newSecretVersion string
	testUpdateSecret(t, gSecretFetcher, k8sTestTLSSecretC, k8sTestTLSSecretD, expectedUpdateSecret, &newSecretVersion)
	if secretVersion == newSecretVersion {
		t.Errorf("updated secret should have different version")
	}
}

// TestSecretFetcherUsingFallbackIngressSecret verifies that if a fall back secret is provided,
// the fall back secret will be returned when real secret is not added, or is already deleted.
func TestSecretFetcherUsingFallbackIngressSecret(t *testing.T) {
	gSecretFetcher := &SecretFetcher{
		UseCaClient:        false,
		DeleteCache:        func(secretName string) {},
		UpdateCache:        func(secretName string, ns security.SecretItem) {},
		FallbackSecretName: k8sSecretFallbackScrt,
	}
	gSecretFetcher.InitWithKubeClient(fake.NewSimpleClientset().CoreV1())
	if gSecretFetcher.UseCaClient {
		t.Error("secretFetcher should not use ca client")
	}
	ch := make(chan struct{})
	gSecretFetcher.Run(ch)

	gSecretFetcher.scrtAdded(k8sTestTLSFallbackSecret)

	// Since we have enabled fallback secret, searching a non-existing secret should return true now.
	fallbackSecret, ok := gSecretFetcher.FindGatewaySecret(k8sSecretNameA)
	if !ok {
		t.Error("secretFetcher should return fallback secret for non-existing-secret")
	}
	if fallbackSecret.ResourceName != k8sSecretFallbackScrt {
		t.Errorf("secret name does not match, expected %v but got %v", k8sSecretFallbackScrt,
			fallbackSecret.ResourceName)
	}
	if !bytes.Equal(k8sFallbackCertChain, fallbackSecret.CertificateChain) {
		t.Errorf("cert chain verification error: expected %v but got %v",
			k8sFallbackCertChain, fallbackSecret.CertificateChain)
	}
	if !bytes.Equal(k8sFallbackKey, fallbackSecret.PrivateKey) {
		t.Errorf("private key verification error: expected %v but got %v",
			k8sFallbackKey, fallbackSecret.PrivateKey)
	}

	// Add test secret and verify that key/cert pair is stored.
	expectedAddedSecrets := []expectedSecret{
		{
			exist: true,
			secret: &security.SecretItem{
				ResourceName:     k8sSecretNameA,
				CertificateChain: k8sCertChainA,
				ExpireTime:       k8sTestCertChainExpireTimeA,
				PrivateKey:       k8sKeyA,
			},
		},
		{
			exist: true,
			secret: &security.SecretItem{
				ResourceName: k8sSecretNameA + GatewaySdsCaSuffix,
				RootCert:     k8sCaCertA,
				ExpireTime:   k8sTestCaCertExpireTimeA,
			},
		},
	}
	var secretVersionOne string
	testAddSecret(t, gSecretFetcher, k8sTestGenericSecretA, expectedAddedSecrets, &secretVersionOne)

	// Delete k8sSecretNameA and verify that FindGatewaySecret returns fall back secret.
	gSecretFetcher.scrtDeleted(k8sTestGenericSecretA)
	fallbackSecret, ok = gSecretFetcher.FindGatewaySecret(k8sSecretNameA)
	if !ok {
		t.Errorf("secretFetcher should return fallback secret for secret %v", k8sSecretNameA)
	}
	if fallbackSecret.ResourceName != k8sSecretFallbackScrt {
		t.Errorf("secret name does not match, expected %v but got %v", k8sSecretFallbackScrt,
			fallbackSecret.ResourceName)
	}
	if !bytes.Equal(k8sFallbackCertChain, fallbackSecret.CertificateChain) {
		t.Errorf("cert chain verification error: expected %v but got %v",
			k8sFallbackCertChain, fallbackSecret.CertificateChain)
	}
	if !bytes.Equal(k8sFallbackKey, fallbackSecret.PrivateKey) {
		t.Errorf("private key verification error: expected %v but got %v",
			k8sFallbackKey, fallbackSecret.PrivateKey)
	}

	// Add test secret again and with different key, verify that key/cert pair is stored and version number is different.
	var secretVersionTwo string
	testAddSecret(t, gSecretFetcher, k8sTestGenericSecretA, expectedAddedSecrets, &secretVersionTwo)
	if secretVersionTwo == secretVersionOne {
		t.Errorf("added secret should have different version")
	}

	// Update test secret and verify that key/cert pair is changed and version number is different.
	expectedUpdateSecrets := []expectedSecret{
		{
			exist: true,
			secret: &security.SecretItem{
				ResourceName:     k8sSecretNameA,
				CertificateChain: k8sCertChainB,
				ExpireTime:       k8sTestCertChainExpireTimeA,
				PrivateKey:       k8sKeyB,
			},
		},
		{
			exist: true,
			secret: &security.SecretItem{
				ResourceName: k8sSecretNameA + GatewaySdsCaSuffix,
				ExpireTime:   k8sTestCaCertExpireTimeA,
				RootCert:     k8sCaCertB,
			},
		},
	}
	var secretVersionThree string
	testUpdateSecret(t, gSecretFetcher, k8sTestGenericSecretA, k8sTestGenericSecretB, expectedUpdateSecrets, &secretVersionThree)
	if secretVersionThree == secretVersionTwo || secretVersionThree == secretVersionOne {
		t.Errorf("updated secret should have different version")
	}
}

func compareSecret(t *testing.T, secret, expectedSecret *security.SecretItem) {
	t.Helper()
	if expectedSecret.ResourceName != secret.ResourceName {
		t.Errorf("resource name verification error: expected %s but got %s", expectedSecret.ResourceName, secret.ResourceName)
	}
	if !bytes.Equal(expectedSecret.CertificateChain, secret.CertificateChain) {
		t.Errorf("cert chain verification error: expected %v but got %v", expectedSecret.CertificateChain, secret.CertificateChain)
	}
	if !bytes.Equal(expectedSecret.PrivateKey, secret.PrivateKey) {
		t.Errorf("private key verification error: expected %v but got %v", expectedSecret.PrivateKey, secret.PrivateKey)
	}
	if !bytes.Equal(expectedSecret.RootCert, secret.RootCert) {
		t.Errorf("root cert verification error: expected %v but got %v", expectedSecret.RootCert, secret.RootCert)
	}
}

func testAddSecret(t *testing.T, sf *SecretFetcher, k8ssecret *v1.Secret, expectedSecrets []expectedSecret, version *string) {
	t.Helper()
	// Add a test secret and find the secret.
	sf.scrtAdded(k8ssecret)
	for _, es := range expectedSecrets {
		secret, ok := sf.FindGatewaySecret(es.secret.ResourceName)
		if es.exist != ok {
			t.Errorf("Unexpected secret %s, expected to exist: %v but got: %v", es.secret.ResourceName, es.exist, ok)
		}
		if es.exist {
			*version = secret.Version
			compareSecret(t, &secret, es.secret)
		}
	}
}

func testDeleteSecret(t *testing.T, sf *SecretFetcher, k8ssecret *v1.Secret, expectedSecrets []expectedSecret) {
	// Delete a test secret and find the secret.
	sf.scrtDeleted(k8ssecret)
	for _, es := range expectedSecrets {
		_, ok := sf.FindGatewaySecret(es.secret.ResourceName)
		if ok {
			t.Errorf("secretFetcher found a deleted secret %v", es.secret.ResourceName)
		}
	}
}

func testUpdateSecret(t *testing.T, sf *SecretFetcher, k8sOldsecret, k8sNewsecret *v1.Secret, expectedSecrets []expectedSecret, version *string) {
	// Add a test secret and find the secret.
	sf.scrtUpdated(k8sOldsecret, k8sNewsecret)
	for _, es := range expectedSecrets {
		secret, ok := sf.FindGatewaySecret(es.secret.ResourceName)
		if es.exist != ok {
			t.Errorf("secretFetcher failed to find secret %s, expected to exist: %v but got: %v", es.secret.ResourceName, es.exist, ok)
		}
		if es.exist {
			*version = secret.Version
			compareSecret(t, &secret, es.secret)
		}
	}
}
