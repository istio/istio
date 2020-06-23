// Copyright Istio Authors. All Rights Reserved.
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

package spiffe

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var (
	validSpiffeX509Bundle = `
{
	"spiffe_sequence": 1,
	"spiffe_refresh_hint": 450000,
	"keys": [
		{
		"kty": "RSA",
		"use": "x509-svid",
		"n": "r10W2IcjT-vvSTpaFsS4OAcPOX87kw-zKZuJgXhxDhkOQyBdPZpUfK4H8yZ2q14Laym4bmiMLocIeGP70k` +
		`UXcp9T4SP-P0DmBTPx3hVgP3YteHzaKsja056VtDs9kAufmFGemTSCenMt7aSlryUbLRO0H-__fTeNkCXR7uIoq` +
		`RfU6jL0nN4EBh02q724iGuX6dpJcQam5bEJjq6Kn4Ry4qn1xHXqQXM4o2f6xDT13sp4U32stpmKh0HOd1WWKr0W` +
		`RYnAh4GnToKr21QySZi9QWTea3zqeFmti-Isji1dKZkgZA2S89BdTWSLe6S_9lV0mtdXvDaT8RmaIX72jE_Abhn` +
		`bUYV84pNYv-T2LtIKoi5PjWk0raaYoexAjtCWiu3PnizxjYOnNwpzgQN9Qh_rY2jv74cgzG50_Ft1B7XUiakNFx` +
		`AiD1k6pNuiu4toY0Es7qt1yeqaC2zcIuuV7HUv1AbFBkIdF5quJHVtZ5AE1MCh1ipLPq-lIjmFdQKSRdbssVw8y` +
		`q9FtFVyVqTz9GnQtoctCIPGQqmJDWmt8E7gjFhweUQo-fGgGuTlZRl9fiPQ6luPyGQ1WL6wH79G9eu4UtmgUDNw` +
		`q7kpYq0_NQ5vw_1WQSY3LsPclfKzkZ-Lw2RVef-SFVVvUFMcd_3ALeeEnnSe4GSY-7vduPUAE5qMH7M",
		"e": "AQAB",
		"x5c": ["MIIGlDCCBHygAwIBAgIQEW25APa7S9Sj/Nj6V6GxQTANBgkqhkiG9w0BAQsFADCBwTELMAkGA1UEBhM` +
		`CVVMxEzARBgNVBAgTCkNhbGlmb3JuaWExFjAUBgNVBAcTDU1vdW50YWluIFZpZXcxEzARBgNVBAoTCkdvb2dsZS` +
		`BMTEMxDjAMBgNVBAsTBUNsb3VkMWAwXgYDVQQDDFdpc3Rpb192MV9jbG91ZF93b3JrbG9hZF9yb290LXNpZ25lc` +
		`i0wLTIwMTgtMDQtMjVUMTQ6MTE6MzMtMDc6MDAgSzoxLCAxOkg1MnZnd0VtM3RjOjA6MTgwIBcNMTgwNDI1MjEx` +
		`MTMzWhgPMjExODA0MjUyMjExMzNaMIHBMQswCQYDVQQGEwJVUzETMBEGA1UECBMKQ2FsaWZvcm5pYTEWMBQGA1U` +
		`EBxMNTW91bnRhaW4gVmlldzETMBEGA1UEChMKR29vZ2xlIExMQzEOMAwGA1UECxMFQ2xvdWQxYDBeBgNVBAMMV2` +
		`lzdGlvX3YxX2Nsb3VkX3dvcmtsb2FkX3Jvb3Qtc2lnbmVyLTAtMjAxOC0wNC0yNVQxNDoxMTozMy0wNzowMCBLO` +
		`jEsIDE6SDUydmd3RW0zdGM6MDoxODCCAiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBAK9dFtiHI0/r70k6` +
		`WhbEuDgHDzl/O5MPsymbiYF4cQ4ZDkMgXT2aVHyuB/MmdqteC2spuG5ojC6HCHhj+9JFF3KfU+Ej/j9A5gUz8d4` +
		`VYD92LXh82irI2tOelbQ7PZALn5hRnpk0gnpzLe2kpa8lGy0TtB/v/303jZAl0e7iKKkX1Ooy9JzeBAYdNqu9uI` +
		`hrl+naSXEGpuWxCY6uip+EcuKp9cR16kFzOKNn+sQ09d7KeFN9rLaZiodBzndVliq9FkWJwIeBp06Cq9tUMkmYv` +
		`UFk3mt86nhZrYviLI4tXSmZIGQNkvPQXU1ki3ukv/ZVdJrXV7w2k/EZmiF+9oxPwG4Z21GFfOKTWL/k9i7SCqIu` +
		`T41pNK2mmKHsQI7Qlortz54s8Y2DpzcKc4EDfUIf62No7++HIMxudPxbdQe11ImpDRcQIg9ZOqTboruLaGNBLO6` +
		`rdcnqmgts3CLrlex1L9QGxQZCHReariR1bWeQBNTAodYqSz6vpSI5hXUCkkXW7LFcPMqvRbRVclak8/Rp0LaHLQ` +
		`iDxkKpiQ1prfBO4IxYcHlEKPnxoBrk5WUZfX4j0Opbj8hkNVi+sB+/RvXruFLZoFAzcKu5KWKtPzUOb8P9VkEmN` +
		`y7D3JXys5Gfi8NkVXn/khVVb1BTHHf9wC3nhJ50nuBkmPu73bj1ABOajB+zAgMBAAGjgYMwgYAwDgYDVR0PAQH/` +
		`BAQDAgEGMB0GA1UdJQQWMBQGCCsGAQUFBwMBBggrBgEFBQcDAjAPBgNVHRMBAf8EBTADAQH/MB0GA1UdDgQWBBQ` +
		`/VsuyjgRDAEmcZjyJ77619Js9ijAfBgNVHSMEGDAWgBQ/VsuyjgRDAEmcZjyJ77619Js9ijANBgkqhkiG9w0BAQ` +
		`sFAAOCAgEAUc5QJOqxmMJY0E2rcHEWQYRah1vat3wuIHtEZ3SkSumyj+y9eyIHb9XTTyc4SyGyX1n8Rary8oSgQ` +
		`V4cbyJTFXEEQOGLHB9/98EKThgJtfPsos2WKe/59S8yN05onpxcaL9y4S295Kv9kcSQxLm5UfjlqsKeHJZymvxi` +
		`YzmBox7LA1zqcLYZvslJNkJxKAk5JA66iyDSQqOK7jIixn8pi305dFGCZglUFStwWqY6Rc9rR8EycVhSx2AhrvT` +
		`7OQTVdKLfoKA84D8JZJPB7hrxqKf7JJFs87Kjt7c/5bXPFJ2osmjoNYnbHjiq64bh20sSCd630qvhhePLwjjOlB` +
		`PiFyK36o/hQN871AEm1SCHy+aQcfJqF5KTgPnZQy5D+D/CGau+BfkO+WCGDVxRleYBJ4g2NbATolygB2KWXrj07` +
		`U/WaWqV2hERbkmxXFh6cUdlkX2MeoG4v6ZD2OKAPx5DpJCfp0TEq6PznP+Z1mLd/ZjGsOF8R2WGQJEuU8HRzvsr` +
		`0wsX9UyLMqf5XViDK11V/W+dcIvjHCayBpX2se3dfex5jFht+JcQc+iwB8caSXkR6tGSiargEtSJODORacO9IB8` +
		`b6W8Sm//JWf/8zyiCcMm1i2yVVphwE1kczFwunAh0JB896VaXGVxXeKEAMQoXHjgDdCYp8/Etxjb8UkCmyjU="]
		}
	]
}`

	invalidSpiffeX509Bundle = `
{
	"spiffe_sequence": 1,
	"spiffe_refresh_hint": 450000,
	"keys": [
		{
		"kty": "RSA",
		"use": "x509-svid",
		"n": "r10W2IcjT-vvSTpaFsS4OAcPOX87kw-zKZuJgXhxDhkOQyBdPZpUfK4H8yZ2q14Laym4bmiMLocIeGP70k` +
		`UXcp9T4SP-P0DmBTPx3hVgP3YteHzaKsja056VtDs9kAufmFGemTSCenMt7aSlryUbLRO0H-__fTeNkCXR7uIoq` +
		`RfU6jL0nN4EBh02q724iGuX6dpJcQam5bEJjq6Kn4Ry4qn1xHXqQXM4o2f6xDT13sp4U32stpmKh0HOd1WWKr0W` +
		`RYnAh4GnToKr21QySZi9QWTea3zqeFmti-Isji1dKZkgZA2S89BdTWSLe6S_9lV0mtdXvDaT8RmaIX72jE_Abhn` +
		`bUYV84pNYv-T2LtIKoi5PjWk0raaYoexAjtCWiu3PnizxjYOnNwpzgQN9Qh_rY2jv74cgzG50_Ft1B7XUiakNFx` +
		`AiD1k6pNuiu4toY0Es7qt1yeqaC2zcIuuV7HUv1AbFBkIdF5quJHVtZ5AE1MCh1ipLPq-lIjmFdQKSRdbssVw8y` +
		`q9FtFVyVqTz9GnQtoctCIPGQqmJDWmt8E7gjFhweUQo-fGgGuTlZRl9fiPQ6luPyGQ1WL6wH79G9eu4UtmgUDNw` +
		`q7kpYq0_NQ5vw_1WQSY3LsPclfKzkZ-Lw2RVef-SFVVvUFMcd_3ALeeEnnSe4GSY-7vduPUAE5qMH7M",
		"e": "AQAB"
		}
	]
}`

	// validRootCert, validIntCert and validWorkloadCert are in a certification chain.
	// They are generated using tools/certs/Makefile. Replace "cluster.local" with "foo.doamin.com"
	// export INTERMEDIATE_DAYS=3650
	// export WORKLOAD_DAYS=3650
	// make foo-certs-selfSigned
	// TODO(myidpt): put the following data into files in security/pkg/pki/testdata.
	validRootCert = `-----BEGIN CERTIFICATE-----
MIIC3TCCAcWgAwIBAgIQVtsuf26VCWmMjpm8BHnNwzANBgkqhkiG9w0BAQsFADAY
MRYwFAYDVQQKEw1jbHVzdGVyLmxvY2FsMB4XDTIwMDYyMjA1NTYyNloXDTMwMDYy
MDA1NTYyNlowGDEWMBQGA1UEChMNY2x1c3Rlci5sb2NhbDCCASIwDQYJKoZIhvcN
AQEBBQADggEPADCCAQoCggEBAOBT/NPvk3OJSMxhpgL/iov1uKBpJP7nub96dUUd
2Psgptq8om+FxSRWpi0jTHR40RewbD/h5Lmp/Rg1bk3CnaKEsSZPaKK35rlc5P15
UFxbXzpER0mLObYgJICQt/PUIEqqY2h8HNJJMMvOU4Bo0+vfZYYMxx4J5D/ADVwz
Pza8TGrSuYSuviJQ6CTMxquXRr/W4LV0lS6nB9ejB3hMHwC6wlWZc+d8rQHiHMDB
k6IaXKEk/X/2Iv8ril1wQSvJ3fdZK8EDE/wMIfiuqoDgFq2rXyGqdXbH5Rd8+MkM
HmKKmRgEzSl+BI6AHO22/MvOC1o0U1wV1Z3NMCGjnlRTkP0CAwEAAaMjMCEwDgYD
VR0PAQH/BAQDAgIEMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEB
AACc+eVqxbrbvBxDAn6vXVNKBPT2jp8veH5ck5ZLbw6vcXLDTDzMS0WD1jw8I9x9
DZqeCU2oC/wtu1DCq3NfXmj4wA8qzIoyvDqXZOWy+ApeLT9FOs3RJfEVISIfFFKB
DMyODbHlR2d1zIGI0sIeFFQkGHxWCSgzzqsgkU8CbalCUGAP19R/q+hnv42k5GP0
5rDPz7evhvDub6W7Y54zZviZpJlZi6ny4j1RXKDcUalYCNAc3NeQ0KwGmEjax7cp
9FGSO7WSIU5yLqiY9Y5iv4+NFi/mfSrDK+KMHX9R1ftOO1GbsPVui9EUHlm8B1Zr
Fvby2wkBJ6hlna8o8VvqZiA=
-----END CERTIFICATE-----
`

	validRootCert2 = `-----BEGIN CERTIFICATE-----
MIIFFDCCAvygAwIBAgIUGLivv//XRngNF57FUlQriSvictgwDQYJKoZIhvcNAQEL
BQAwIjEOMAwGA1UECgwFSXN0aW8xEDAOBgNVBAMMB1Jvb3QgQ0EwHhcNMjAwNjIw
MDM1MzQyWhcNMzAwNjE4MDM1MzQyWjAiMQ4wDAYDVQQKDAVJc3RpbzEQMA4GA1UE
AwwHUm9vdCBDQTCCAiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBAPLD7+JV
nXe2koF5LBBGaKXOB79FymckNvHK826wTF3Ni+QvDKEazteC83ZKTJs+CLarXL27
KZkpJLXo2UTaXIIms1aRJB2mcfsFwruTmd4zbWUq50nztHA5qbyFcrdQ+n7cgHD5
LJUUIQON9ZxvwH/ilA9yniERK8Tn2sHYjs5UiiJAIDLLuoHpq8sBq9mjiWp5aco7
igEiwQXIGLMq2UmIJhFSElsTXPMWYHPhJxIPNfg+FsOQl8xW5W3B3ktnc//eY4iB
JkHpi/O1PuOgvqsuiD/9O7adfPAAvltUloaUNjE1BZUONPPU/GDyrMtCmBUlejeo
i+8Tz1HdPG2iII3+eIL+W5BDxHaQuAASdjjBi+yA/bJraeCSk88B9qANHNcxO9aY
ul9aVmqs9PYG8lENKmo/5tRf0Smny3vz7N21LVzUf/nt4ehSgnzg+yUzdQc5XCxR
lKyfqBftxt/ijXRyN5dA+ASiY8jzGQUSZp5vo4u2TVzT1QCgyTu8wZhNnWTGbcBa
/ahgqkXZbJYLxnZP1YrjquVJ8BQ/lvErzn9L9q+oMswYIzBRB4wCbkNlvpWdLwAm
zlMz0Mdk40BB2DKLI/19htxNnTNSnEokRAFnXQJtOKFYx7laKTqgpotx2ULOapyX
oD3eBTuTdUL/9IRTE396jJKzYdjok88xH/zBAgMBAAGjQjBAMB0GA1UdDgQWBBQ3
/eYN0LrpnLSB+qR6eq18tRhYuTAPBgNVHRMBAf8EBTADAQH/MA4GA1UdDwEB/wQE
AwIC5DANBgkqhkiG9w0BAQsFAAOCAgEA7FuADmil5NagBixMI5lR36mviNeBfzmF
De0QbhGa1AzAxraGDpDYrKemLPp+yxsdMhJQ+Y9ZGkx+CrSE0Ltx2C4Im88xGJTm
xuNSikTQc3q7DJniUHkI2EdgTRMZFDjN+kE6fcnU8G0ktVRX5HfEwAdrjFau++mS
/p07C3yMfDd6xPBtCMUgFGdEG52cozhCxilO6nIq4gnOS6YxZk09V3tElmU5neu9
z9+4PfOZavnpN2OLFVGnRB+AR/S9bvkvxeBzcw6GhwtAoSBaPAYAzjJKKS7+i/lB
SdCt6OwMmc4wchO/Wm7o4dcm29HGurs8qkMheYZYHdD7N/TzMV0fb+2Yy/9tJtSF
2LkhC7s4l1oZxB1oAJ/5w0Hk+apJo4+44MEw1DWaowTOfgCdHCSLu8X4AzJo29xv
dGR87iX3yyBpU+74FQTFDuhzFTca1qfUU+zS/R7+fC2pFy9aqfC901j+VKHZuq96
HgIIyGL6SQ/kaqmdSut3NsRKBGdO/3jONFVo/60vyAXqOGs7n5cRXr8t15tzmTRs
EdcbproYFIvbdtt4PzFReeGetkxIm+m+qTqSEZC5DjkYdAGQ6dbdZZ23JZXWwqie
M5TiKE/OkSVZuNDHOyxNE44HxpwoKJ7sc31/DQkgqd8Zah6VZ/8f3oPZUIypUsoy
TU41f1r+IIo=
-----END CERTIFICATE-----
`

	validIntCert = `-----BEGIN CERTIFICATE-----
MIIERzCCAy+gAwIBAgIUKqje+ayLqaFyCRmVG2/w8wgXg34wDQYJKoZIhvcNAQEL
BQAwGDEWMBQGA1UEChMNY2x1c3Rlci5sb2NhbDAeFw0yMDA2MjIxODQ2MDVaFw0z
MDA2MjAxODQ2MDVaMDgxDjAMBgNVBAoMBUlzdGlvMRgwFgYDVQQDDA9JbnRlcm1l
ZGlhdGUgQ0ExDDAKBgNVBAcMA2ZvbzCCAiIwDQYJKoZIhvcNAQEBBQADggIPADCC
AgoCggIBAMPaYGC9aaHz8omAtD6gn30iqtQ05gq8TCrokOcDtJ5a6qjdgxz+c8kT
4aWM/VXE1uEg6IiAcmnfxBVV6hYdn0+q5FjSo/68QuktzF7/zSYpLB0YFA4biYle
ADrqveweJCVP3pjVYw4yhP7g3BxCJDMjCZJk5Zed8r/WLYa7EOVxL9b7s/lnO+x0
1sQbO6hLfUr+W9hBcfs/KUgzsmTuuBtQmyN75npzwFJCQ1VcoHZvLSf/PO8NEyWz
QWQGkkHQrpol7/gV7mpoXnTkmYM0cd7H1wfb9khc0IalXK+zFWcvhtKQQBcaoy8j
RFNodTzarTdW1Y2WpZv/q7ovv+E8Y5bCaKNDggIlMjLV65Do67vcngh00Fh28zs1
m5PKm2uUP3ntF4wD+c0QCEWs2KltIrh/NFLoxWw69VOw4+n8QAcN9CF+Bd/EKqNY
zlKZgx9qGAPG9YEDfEKEjfDiirg8SwosYpk9YOmHKZaUPY5xs+lmYUJOT9RxdLxs
gCvOmKYhdMEm/NmQeFIuZhvtGWj9zh8fEeTRo4wxJP22FHftAovcxSZ6wLa+AjOt
oydzJmFzmA94Fhjx7hdaFGt0YB1INBi2ZM8mXeBGjzT3ZI7M0QI0KKOT2AqTAMqK
GKoWa21mXJUgif+Fws1NgzPUR5dMpMgKN2WT7A/Ojam/H/QTflzLAgMBAAGjaTBn
MB0GA1UdDgQWBBT4mvBtW6QH26Hg3nftibjI7um9DzASBgNVHRMBAf8ECDAGAQH/
AgEAMA4GA1UdDwEB/wQEAwIC5DAiBgNVHREEGzAZghdpc3Rpb2QuaXN0aW8tc3lz
dGVtLnN2YzANBgkqhkiG9w0BAQsFAAOCAQEAOPHaVXDxBxflaGb51jdMDZb4H4Se
ziF7El+2UDvxeI8OPQajf1mh8Ymxdw5LrlmDv8sEzzcGkTqs1RQo4/6o1f0hSLKd
i2FnDHoMFfzEjFZngXnVpk41GYq5us56qi9QKrLkQ+bMVaKyGLBA0cjDvzvHb34l
MG1YAKfEziuFofyRf5a4+OMeV5BY+kq5cG9x3lqJJVbrzuNjA2uVwce28IYhwuwu
lh3viBgxxvwls9CmVQncYLjaxc2M3OS2H2tJCNDmvs56k++8FGcV/x4D1tLNlBfd
mIKLKown8GW7MD4ZS4dP0oHlLGJXZF/i3MXmTJ1sJjU1FRU+KsrvFBYLcg==
-----END CERTIFICATE-----
`

	validWorkloadCert = `-----BEGIN CERTIFICATE-----
MIIFszCCA5ugAwIBAgIUZfElJVAAYMZJyht+H/sfXI7iEr0wDQYJKoZIhvcNAQEL
BQAwODEOMAwGA1UECgwFSXN0aW8xGDAWBgNVBAMMD0ludGVybWVkaWF0ZSBDQTEM
MAoGA1UEBwwDZm9vMB4XDTIwMDYyMjE4NDYwNVoXDTMwMDYyMDE4NDYwNVowODEO
MAwGA1UECgwFSXN0aW8xGDAWBgNVBAMMD0ludGVybWVkaWF0ZSBDQTEMMAoGA1UE
BwwDZm9vMIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEA2OXFXjACONaJ
h1rvBDAdj5CKMoceiQGEAU/KRvCSz2wl8ydI4X1dngec+RhmgsxhdSAOORNwHBJv
mHEPODbOETtfcm2fD70IL/8M1dbRX9Cazuli4m2Wesp6QJViSykR0pmaCe8Xx1SL
w5Dv8/sHGfav3LHHxvAzIyWfNUoWQHNFUZdSUOcze74J0A7W5C3HTkf34sjmzetG
TC+amRhKRJAESJtucFc2+nlLEmGGnF8nZuGoWB6w7UJCREzyEx9NDtlSCbq37KAY
Vto4m6rZRrJknc652MQPTl25trb959+VMWj4/p813ihKZ2f9R7Mses91+5hJCHI5
ZsYdYoc3TPBQUBeAevDwlAaTMsWBzuMSsgro6sRhd4+U0TeVP2uoyk18dia2rODt
kqsovTy2fxb9JAjMlwLsGL9LGmWevCx5YbDtRyGIWVUzq6MARKtyFJwQBKmVokN0
huETJ7o4eSxigIQm0B7JiFEjQi/x4eG2JLH9+K821nw4/YD26XYn18XFcUwxAttM
YilgD8xdaYqxmQahnKoNlVavIfLQO5NJnWm2nZ92JxFXdjAtNLuUkuoOT7whKun/
FjAEu4pljqo+AajzAWEIL5RLU7mpXYe2GPKIZESGQxOPeU2PnKdNhCiNp6OxaSDd
nekC+4+DNJvPVX2u7ATOm8xBtkQH6ccCAwEAAaOBtDCBsTAdBgNVHQ4EFgQUY1nh
gotEdZKG/Ha3hAbPknCt29wwDAYDVR0TAQH/BAIwADALBgNVHQ8EBAMCBaAwHQYD
VR0lBBYwFAYIKwYBBQUHAwEGCCsGAQUFBwMCMFYGA1UdEQRPME2GKXNwaWZmZTov
L2Zvby5kb21haW4uY29tL25zL2Zvby9zYS9kZWZhdWx0giBmb28uZG9tYWluLmNv
bS9ucy9mb28vc2EvZGVmYXVsdDANBgkqhkiG9w0BAQsFAAOCAgEAMk6kdBnSO69P
xnRBn+VFJ7VpmlGSgoEI2nNUkfTFNKSERunz+KP9o9edTMZ6QqJpnCLG6DC8S1Jt
L2r/EVXspun1qdFivq4qN7o5tqE9FWIFEroERpfVRLlAjwttzHeXqQONLa19tlIq
xsjGcZE1XQ0nGn2pfADbr3ZntIxA9xVydFiNbVAYiy+Fx6Gv8tmLnc+79QHPUwjX
Sb3rhmmSTbUjFVe/TWZ7HDkwrSnW4mPl7rklBUcYTvLPEQhzkU8+kvgaVCYE/A7Y
ClXEpsaJXMoFzTdN9CGK6B24gGXlTEfpsCdKWmqguXlz3F5hFZ7ueyJ4LTjNWa75
lxi5kE5hNzsyCSm2/qGeeGXdHbxuOVY22Mb82I5AKiziMXEFG5NiA9V7o+W6op4O
v0OhCdMlT236ePABIWwxuktRFxNhfMvIMOjiq078rfDmEDMizM4mtgm0Hcd+zflv
4Lz8y+ZuAaucqTgHaBiSSzkhCZR/nSKWh6sYMAuTgFxa2Y6OqRCJ+mfXstfZWuOZ
esHy2qkI/hS7lxTI8fdww1vsQGc51N7lg2l0KxNjDMwwwJ9PQ9HCIvl1N8lb0523
PHMWzScC8lxwrp5R2oMfuMFaKbPX5iHPb85x95beJ1hbq4D0KGtwjv7yxaWWhAxP
RstSYuX5neeOWBugXmv/lWqFk+TkdV0=
-----END CERTIFICATE-----
`

	validWorkloadKey = `-----BEGIN RSA PRIVATE KEY-----
MIIJKQIBAAKCAgEA2OXFXjACONaJh1rvBDAdj5CKMoceiQGEAU/KRvCSz2wl8ydI
4X1dngec+RhmgsxhdSAOORNwHBJvmHEPODbOETtfcm2fD70IL/8M1dbRX9Cazuli
4m2Wesp6QJViSykR0pmaCe8Xx1SLw5Dv8/sHGfav3LHHxvAzIyWfNUoWQHNFUZdS
UOcze74J0A7W5C3HTkf34sjmzetGTC+amRhKRJAESJtucFc2+nlLEmGGnF8nZuGo
WB6w7UJCREzyEx9NDtlSCbq37KAYVto4m6rZRrJknc652MQPTl25trb959+VMWj4
/p813ihKZ2f9R7Mses91+5hJCHI5ZsYdYoc3TPBQUBeAevDwlAaTMsWBzuMSsgro
6sRhd4+U0TeVP2uoyk18dia2rODtkqsovTy2fxb9JAjMlwLsGL9LGmWevCx5YbDt
RyGIWVUzq6MARKtyFJwQBKmVokN0huETJ7o4eSxigIQm0B7JiFEjQi/x4eG2JLH9
+K821nw4/YD26XYn18XFcUwxAttMYilgD8xdaYqxmQahnKoNlVavIfLQO5NJnWm2
nZ92JxFXdjAtNLuUkuoOT7whKun/FjAEu4pljqo+AajzAWEIL5RLU7mpXYe2GPKI
ZESGQxOPeU2PnKdNhCiNp6OxaSDdnekC+4+DNJvPVX2u7ATOm8xBtkQH6ccCAwEA
AQKCAgAplMMlr2Z9pwNuo4w27VJ9d2RHE4hTE6tO5REOUIiUo1MTLnDWacZMyYDa
cEcWxD/ayG5xmrxfZVlnjCUyza7rtsoxkbpwtfif2vGG/UveZouHJ08Bwaibmb2e
LAVQC2uTSEczqFaSrC6vK1YVHAbcf2JvmNWH2fyzvD6tZKqnaHHdlnj9cZV5H5Ga
BX5E+FHBPCLVo1Y8G+K6MFYfC30Rb9qiYMnnV5D+q8osl+3KhKN1IcW4PwoEMjOq
DGZMLDAFrLwBiX5BKt//po47qaFF4GVRq5QNbmjQyT8VPDepAEAF3O3/Ql59XJQH
BvSTjlH0qVkhBqzZpaxDe6+ed/Wtu70f1d9QhZmn9kKgRq77Mc+O435bUwzlXLKf
Z+M5i2JHbqsyQajm2xUFWfVs7i+vUD1P5Wa4X7LsDlj7yOoGcXcIvN+ixVhsJgSe
VJBFa7FeUYn6/AepdszUToW1WnkuusfFPf/EyuRZTL6igGaBFylTRe+QS1cTjlny
M+5lQPc5fWX60I8zhPMgmNDmuKw368Tp2EJKR9BkokB7gOhAwOe3Xm/o0q9zxBVB
t5GKqMtrB2vcPLheyoZlPg5zLOS1oMdKLAN64yYhFgCelVB4VsPkWy/RFKgJS2cr
3yVyTyNX+MoUsMHPhq17Oy7RF7RHqC/JADn9c8DVzXG+RyiNgQKCAQEA/YMT5dWr
Rrgv9981zrqLRHEsriS6oZYm0N+urobD4UlBmLRb2KLvJWC13yBlZ1oYWkvWGj+O
vT6ti0cPEEFurbJWoc63wM3OfZVKEQ4nRmtBsp+riUw7MdNT/7HcKkkANTua3gcq
UidIN0mQvSEmsXysCD9sgOowEh72VEIViK+7ttO94aduTJ1j0TBhIBPWfhHzcZ1G
DGowp5fXrlFnN4/IsTCp3pz7qxjI62rWsk0EFyem1vXVfOqMDZ4iuKlajgmCYjWx
Qf89mrQBNpSqTZr7FDY1ESzSMZp7uPq4RuJr7WFIcGvJdE3q2aoRytHPug5pQLtH
HnuQtOdo84bv0QKCAQEA2wa0Bf/FDQnnCLNdrlZtFqDay+kiAdzUgJpRguPos3RC
g1ZjO4L7WVeNBtIqm3tCogdJebRHy4TquqMYFzj3Mbozz/J5bwISQAINRbAOpLR3
FKETe1pTIc1PVoo2SU1rrmaM2z4nf1iRaxwiGtp7LMCNn9MfUMmynFu1OeXJbUFz
59VqQanakKrOLXLEumiyQ92OVHbMQ0deHB5tNDMpQrHraoo0k5vUXS1ckGirQNJx
LjToyA94QBEKKnnOdiDjt9tF6aE3iQybqTHW6h6cGIWomtVRGJf+yk/Moa07U/8K
Sp303OjeLTvLVY+Yik8dZPv88iceoRT88d7pnkf+FwKCAQEA9VGxoJhqvO2h5ZCH
ZjyYZivKm94JCDLf7wJ17IeW59xW8Omfc30ARMBYXsnftuq1ZDO8xPu6KiGMGJoz
1nwrGUTZlo0OvjGqX1ZnLSfwE7HZCnx+p0cwhR/GSkoYDodD/z9ltvNiHcvLk0zK
FmsNIXXOl9CgNAPrbq2tm42zfujnkp1GQyYdk2A+5oCVjFAGIUtHtCsITR05ZgSG
/zXg1yB5ihXYXAa3dzNtwnpJtpLWoX2KcrvD0rS5wLfFS8L+UTKcjGL+3CmduKX2
ApZMUvrlewKVycAAy2V90lw0lMuouzaHvdpgQP7hg66StxzfkmE9sxlHUhUqzBSf
OAHc8QKCAQBTKbRpIrh7PutOTmyfqYk6MlFhY1/aPTMisXWJsWfF27r1i3OaQR2W
yrttf5dV+fNO+l1XrLAmAo3t18dp6eNSKlVJ+9NH5w1u6FiJwVOODke4uYBgMeem
ygH55fi+1HqyeZW6GVt96u8sMD5y28oxL9uWd99IGY2L+PZSyYE1zshnmo0B6bBn
hbNLZmx0KxSk2BcW0xSz5wFAw/zK+TINdOjiRx+3fE+iIXsoCdYcgssetFA+xkDu
condnupZyBsu0D83elNP4k2obJghxQWX+ggO4jgskmnX/3y/VrtUJV6O/nLe/jx+
CFooXqGYwnlywotElr32g7WXUQB7bPJ5AoIBAQC4XZ32PRRR+2vddhuKzG+8TCEd
tfOd4RFcqM4fG2cX2sLCsH24zTHJ3dC7wInmN6tl6DmA8oWov/YgXRPyNwnqsIPh
uvHF13GvpysaPXjrCDSALF504KrF4KyXbUXdTOrgBB8nF7ZWWa/lfGq/e/TS45pq
iQXSSOA0Uk7I8XeXFnLNatKah8FeyrohtRzPn9O8l9tGx+v0ogCiFRsca8P0PPwN
HtpzFVPSJfWjFDH5Jb6nYdE75GR/sQwr86H50bZpgA6Mq05Y1vniPdCmVtG1NmDG
hGPitf1iUqSP+PmgXtcb2OVrCouNCAlpXLRnRY3DuVbAx4GeYs1FvYXi5aiR
-----END RSA PRIVATE KEY-----
`
)

func TestGenSpiffeURI(t *testing.T) {
	oldTrustDomain := GetTrustDomain()
	defer SetTrustDomain(oldTrustDomain)

	testCases := []struct {
		namespace      string
		trustDomain    string
		serviceAccount string
		expectedError  string
		expectedURI    string
	}{
		{
			serviceAccount: "sa",
			trustDomain:    defaultTrustDomain,
			expectedError:  "namespace or service account empty for SPIFFE uri",
		},
		{
			namespace:     "ns",
			trustDomain:   defaultTrustDomain,
			expectedError: "namespace or service account empty for SPIFFE uri",
		},
		{
			namespace:      "namespace-foo",
			serviceAccount: "service-bar",
			trustDomain:    defaultTrustDomain,
			expectedURI:    "spiffe://cluster.local/ns/namespace-foo/sa/service-bar",
		},
		{
			namespace:      "foo",
			serviceAccount: "bar",
			trustDomain:    defaultTrustDomain,
			expectedURI:    "spiffe://cluster.local/ns/foo/sa/bar",
		},
		{
			namespace:      "foo",
			serviceAccount: "bar",
			trustDomain:    "kube-federating-id@testproj.iam.gserviceaccount.com",
			expectedURI:    "spiffe://kube-federating-id.testproj.iam.gserviceaccount.com/ns/foo/sa/bar",
		},
	}
	for id, tc := range testCases {
		SetTrustDomain(tc.trustDomain)
		got, err := GenSpiffeURI(tc.namespace, tc.serviceAccount)
		if tc.expectedError == "" && err != nil {
			t.Errorf("teste case [%v] failed, error %v", id, tc)
		}
		if tc.expectedError != "" {
			if err == nil {
				t.Errorf("want get error %v, got nil", tc.expectedError)
			} else if !strings.Contains(err.Error(), tc.expectedError) {
				t.Errorf("want error contains %v,  got error %v", tc.expectedError, err)
			}
			continue
		}
		if got != tc.expectedURI {
			t.Errorf("unexpected subject name, want %v, got %v", tc.expectedURI, got)
		}
	}
}

func TestGetSetTrustDomain(t *testing.T) {
	oldTrustDomain := GetTrustDomain()
	defer SetTrustDomain(oldTrustDomain)

	cases := []struct {
		in  string
		out string
	}{
		{
			in:  "test.local",
			out: "test.local",
		},
		{
			in:  "test@local",
			out: "test.local",
		},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			SetTrustDomain(c.in)
			if GetTrustDomain() != c.out {
				t.Errorf("expected=%s, actual=%s", c.out, GetTrustDomain())
			}
		})
	}
}

func TestMustGenSpiffeURI(t *testing.T) {
	if nonsense := MustGenSpiffeURI("", ""); nonsense != "spiffe://cluster.local/ns//sa/" {
		t.Errorf("Unexpected spiffe URI for empty namespace and service account: %s", nonsense)
	}
}

func TestGenCustomSpiffe(t *testing.T) {
	oldTrustDomain := GetTrustDomain()
	defer SetTrustDomain(oldTrustDomain)

	testCases := []struct {
		trustDomain string
		identity    string
		expectedURI string
	}{
		{
			identity:    "foo",
			trustDomain: "mesh.com",
			expectedURI: "spiffe://mesh.com/foo",
		},
		{
			//identity is empty
			trustDomain: "mesh.com",
			expectedURI: "",
		},
	}
	for id, tc := range testCases {
		SetTrustDomain(tc.trustDomain)
		got := GenCustomSpiffe(tc.identity)

		if got != tc.expectedURI {
			t.Errorf("Test id: %v , unexpected subject name, want %v, got %v", id, tc.expectedURI, got)
		}
	}
}

// The test starts one or two local servers and tests RetrieveSpiffeBundleRootCerts is able to correctly retrieve the
// SPIFFE bundles.
func TestRetrieveSpiffeBundleRootCertsFromStringInput(t *testing.T) {
	inputStringTemplate1 := `foo|URL1`
	inputStringTemplate2 := `foo|URL1||bar|URL2`
	totalRetryTimeout = time.Millisecond * 50
	testCases := []struct {
		name        string
		template    string
		trustCert   bool
		status      int
		body        string
		twoServers  bool
		errContains string
	}{
		{
			name:       "success",
			template:   inputStringTemplate1,
			trustCert:  true,
			status:     http.StatusOK,
			body:       validSpiffeX509Bundle,
			twoServers: false,
		},
		{
			name:       "success",
			template:   inputStringTemplate2,
			trustCert:  true,
			status:     http.StatusOK,
			body:       validSpiffeX509Bundle,
			twoServers: true,
		},
		{
			name:        "Invalid input 1",
			template:    "foo||URL1",
			trustCert:   false,
			status:      http.StatusOK,
			body:        validSpiffeX509Bundle,
			twoServers:  false,
			errContains: "config is invalid",
		},
		{
			name:        "Invalid input 2",
			template:    "foo|URL1|bar|URL2",
			trustCert:   false,
			status:      http.StatusOK,
			body:        validSpiffeX509Bundle,
			twoServers:  true,
			errContains: "config is invalid",
		},
		{
			name:        "Invalid input 3",
			template:    "URL1||bar|URL2",
			trustCert:   false,
			status:      http.StatusOK,
			body:        validSpiffeX509Bundle,
			twoServers:  true,
			errContains: "config is invalid",
		},
		{
			name:        "Unauthenticated cert",
			template:    inputStringTemplate1,
			trustCert:   false,
			status:      http.StatusOK,
			body:        validSpiffeX509Bundle,
			twoServers:  false,
			errContains: "x509: certificate signed by unknown authority",
		},
		{
			name:        "non-200 status",
			template:    inputStringTemplate1,
			trustCert:   true,
			status:      http.StatusServiceUnavailable,
			body:        "tHe SYsTEm iS DowN",
			twoServers:  false,
			errContains: "unexpected status: 503, fetching bundle: tHe SYsTEm iS DowN",
		},
		{
			name:        "Certificate absent",
			template:    inputStringTemplate1,
			trustCert:   true,
			status:      http.StatusOK,
			body:        invalidSpiffeX509Bundle,
			twoServers:  false,
			errContains: "expected 1 certificate in x509-svid entry 0; got 0",
		},
		{
			name:        "invalid bundle content",
			template:    inputStringTemplate1,
			trustCert:   true,
			status:      http.StatusOK,
			body:        "NOT JSON",
			twoServers:  false,
			errContains: "failed to decode bundle",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(testCase.status)
				_, _ = w.Write([]byte(testCase.body))
			})
			server := httptest.NewTLSServer(handler)
			input := strings.Replace(testCase.template, "URL1", server.Listener.Addr().String(), 1)
			var trustedCerts []*x509.Certificate
			if testCase.trustCert {
				trustedCerts = append(trustedCerts, server.Certificate())
			}
			if testCase.twoServers {
				input = strings.Replace(testCase.template, "URL1", server.Listener.Addr().String(), 1)
				handler2 := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					w.WriteHeader(testCase.status)
					_, _ = w.Write([]byte(testCase.body))
				})
				server2 := httptest.NewTLSServer(handler2)
				input = strings.Replace(input, "URL2", server2.Listener.Addr().String(), 1)
				if testCase.trustCert {
					trustedCerts = append(trustedCerts, server2.Certificate())
				}

			}
			rootCertMap, err := RetrieveSpiffeBundleRootCertsFromStringInput(input, trustedCerts)
			if testCase.errContains != "" {
				if !strings.Contains(err.Error(), testCase.errContains) {
					t.Errorf("unexpected error returned: %v. The error should contain: %s", err, testCase.errContains)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %s. Expected no error.", err)
			}
			if rootCertMap == nil {
				t.Errorf("returned root cert map is nil")
			}
		})
	}
}

// TestVerifyPeerCert tests VerifyPeerCert is effective at the client side, using a TLS server.
func TestGetGeneralCertPoolAndVerifyPeerCert(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Response body"))
	}))

	workloadCertBlock, _ := pem.Decode([]byte(validWorkloadCert))
	if workloadCertBlock == nil {
		t.Fatalf("failed to decode workload PEM cert")
	}
	intCertBlock, _ := pem.Decode([]byte(validIntCert))
	if intCertBlock == nil {
		t.Fatalf("failed to decode intermediate PEM cert")
	}
	serverCert := [][]byte{workloadCertBlock.Bytes, intCertBlock.Bytes}

	keyBlock, _ := pem.Decode([]byte(validWorkloadKey))
	if keyBlock == nil {
		t.Fatalf("failed to parse PEM block containing the workload private key")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		t.Fatalf("failed to parse workload private key: %v", privateKey)
	}

	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{
			{
				Certificate: serverCert,
				PrivateKey:  privateKey,
			},
		},
	}
	server.StartTLS()
	defer server.Close()

	testCases := []struct {
		name        string
		certMap     map[string][]string
		errContains string
	}{
		{
			name:    "Successful validation",
			certMap: map[string][]string{"foo.domain.com": {validRootCert}},
		},
		{
			name:    "Successful validation with multiple roots",
			certMap: map[string][]string{"foo.domain.com": {validRootCert, validRootCert2}},
		},
		{
			name: "Successful validation with multiple roots multiple mappings",
			certMap: map[string][]string{
				"foo.domain.com": {validRootCert, validRootCert2},
				"bar.domain.com": {validRootCert2},
			},
		},
		{
			name:        "No trusted root CA",
			certMap:     map[string][]string{"foo.domain.com": {validRootCert2}},
			errContains: "x509: certificate signed by unknown authority",
		},
		{
			name:        "Unknown trust domain",
			certMap:     map[string][]string{"bar.domain.com": {validRootCert}},
			errContains: "no cert pool found for trust domain foo.domain.com",
		},
		{
			name: "trustdomain not mapped to the needed root cert",
			certMap: map[string][]string{
				"foo.domain.com": {validRootCert2},
				"bar.domain.com": {validRootCert},
			},
			errContains: "x509: certificate signed by unknown authority",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			certMap := make(map[string][]*x509.Certificate)
			for trustDomain, certStrs := range testCase.certMap {
				certMap[trustDomain] = []*x509.Certificate{}
				for _, certStr := range certStrs {
					block, _ := pem.Decode([]byte(certStr))
					if block == nil {
						t.Fatalf("Can't decode the root cert.")
					}
					rootCert, err := x509.ParseCertificate(block.Bytes)
					if err != nil {
						t.Fatalf("Failed to parse certificate: " + err.Error())
					}
					certMap[trustDomain] = append(certMap[trustDomain], rootCert)
				}
			}

			verifier := NewPeerCertVerifier()
			verifier.AddMappings(certMap)
			if verifier == nil {
				t.Fatalf("Failed to create peer cert verifier.")
			}
			client := &http.Client{
				Timeout: time.Second,
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						RootCAs:               verifier.GetGeneralCertPool(),
						ServerName:            "foo.domain.com/ns/foo/sa/default",
						VerifyPeerCertificate: verifier.VerifyPeerCert,
					},
				},
			}

			req, err := http.NewRequest("POST", "https://"+server.Listener.Addr().String(), bytes.NewBuffer([]byte("ABC")))
			if err != nil {
				t.Errorf("failed to create HTTP client: %v", err)
			}
			_, err = client.Do(req)
			if testCase.errContains != "" {
				if err == nil {
					t.Errorf("Expected error should contain %s but seeing no error.", testCase.errContains)
				} else if !strings.Contains(err.Error(), testCase.errContains) {
					t.Errorf("unexpected error returned: %v. The error should contain: %s", err, testCase.errContains)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %s. Expected no error.", err)
			}
		})
	}
}
