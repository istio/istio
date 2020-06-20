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
	validResponse = `
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

	invalidResponse = `
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

	validRootCert = `-----BEGIN CERTIFICATE-----
MIIFFDCCAvygAwIBAgIUZPTVPiaMEzgkLNJgc1gSAMvxFAwwDQYJKoZIhvcNAQEL
BQAwIjEOMAwGA1UECgwFSXN0aW8xEDAOBgNVBAMMB1Jvb3QgQ0EwHhcNMjAwNjIw
MDEzODAyWhcNMzAwNjE4MDEzODAyWjAiMQ4wDAYDVQQKDAVJc3RpbzEQMA4GA1UE
AwwHUm9vdCBDQTCCAiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBAPiZ+9bs
vlcK7m0TK7CtUoEb/+tjaV3nWeh/o1cLQkLSvI/PNdrQOmVAGhhWGzweOEKbl1PP
dq5PRW2FkZh56M73PR/zj4nqhWpbDkq6N+6cLFGP4G7H90qgvaYxeeWdXWahYF/c
KpL80jF0SffKrmN8jPc2hpNScnmKFMFR1cXjQKPGXTTV4lY+Xe7D/Qs8VuZrK6lO
OUfc89McKDgO7pPYc2Dn2hBzElvYo7IUNB6JyDbnA41iUq+F3BYfUJGIzi040agk
8w4H/tHtnEABDhdm22MFFkeyCXQLqVrglMgaXTdR1y8NWSiZoVZt2YoONTb6ykXo
Ik/ABNacvf8FF+44UW+r4mob0KKBDivTtVNgaELzP5bMjuUbvQqXwdq1boUug0cv
nhrmr9M2cSavgxkztQvp+OGnB6218jCu8Ono36E5+DgrTHOH83uFAkQwgPwurbHY
5wPpqybJB+j+uJP5CkRD8FLQtRm9yg+AYoRNntLUz9TfE4mJ2JsTU3+rq3vSJ5ex
FpyoQ/yo7zuLGvoNf6WUC2rD2dBuYBe0TKpROSw4YR9+PXymlW//vm3m320hAAYi
VOSBpNiI/2x5HDP/ma5/DQBhr4Gp5Pz4nO8R8ynyBYR+uI8Nr2b3vxCQ+btM8LUl
tH06p2lJtJ7tGrbGzjr3TDsg+GW4ENhCNklhAgMBAAGjQjBAMB0GA1UdDgQWBBRr
58OWUphMkd+ribHy9IkUcVUfUzAPBgNVHRMBAf8EBTADAQH/MA4GA1UdDwEB/wQE
AwIC5DANBgkqhkiG9w0BAQsFAAOCAgEAio0y1hfi0SLdoqXqvlQfOdiZzdqm4VLC
dfKoTxx9S8UNQYT0B5ir9Q8W6180UJ45KW4hefRSyrITWHHJg8Sj+yd+agMMY2FT
oTD1MutP8rQQgIkZ4OgNVdT175GFVRSGExCAwaipLHYiCnU5jNyIAtSv8M7Pi5/Y
FudXOYgUMv+HK1R+oAn18Ei5jkXRWGd7p9SNW4v1l23gB4F7iiqPSvR2jzIjP8Xp
Ck2hElA4acslWIQI/PJzGr3KSXfuPyGd5vX0Kd1rOzTPg23YwszdrF9prUt2xa5x
IZyEaLP/O3mVDuCPlOp0pUIWxGc3Uv9jl8xwj18i2m9HXExBLBUp2KXmTrfePKWM
OVZHbGWYQidjfZ5hNc4MNeooLiGPUc1+t0LI6KICTVbuGxfjGsKa0bkvXUKsbArK
1cLV5Kmd5cXxK+GS5dHWPM7FkAFufv/PK3rWzmTmTfThXIBsHM8fIxfLQdKaEXnF
0+++xZGdc63qYiTDNDw0z62NJTlpntyr5evVkAlubkeVWBST3fvpCDA5nNqghy5y
5olmHKOdtKPNdyg3DTXhxbJnFzGXg0g68qTaY/FJ4efb+nnNmgVUl35PM5eD/0FB
4sscffCLwOA7cu1OUWnqB7GMKUncLN7nvFvUdgNLUWcUGl4adZhJ3ybs/Zfrgt/9
nakBEF19U08=
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
MIIFUTCCAzmgAwIBAgIUIaeKbuL2yMmUfh4zzlEYpOmXJCUwDQYJKoZIhvcNAQEL
BQAwIjEOMAwGA1UECgwFSXN0aW8xEDAOBgNVBAMMB1Jvb3QgQ0EwHhcNMjAwNjIw
MDIwOTA3WhcNMzAwNjE4MDIwOTA3WjA4MQ4wDAYDVQQKDAVJc3RpbzEYMBYGA1UE
AwwPSW50ZXJtZWRpYXRlIENBMQwwCgYDVQQHDANmb28wggIiMA0GCSqGSIb3DQEB
AQUAA4ICDwAwggIKAoICAQCzRvqF8P2QFGQ4rrzSiEO4HyUL69pq4hPEcnOvbM6K
qxJk/S66Qsjn5PWbhGVPN7Hne0Z1h999gNfBEODCX3lR2LwtJZYiphxrnq28VUI9
LodiYbKQ12fuKpFh2gtdF8dghoAs3SoS3S2ZpbgauyQ4M5Bnh4SgfyXj/rN2N82J
MifHxvU1izG7OCKs4bNZ9ZoE3O5yzFkysyrGJoYIavuvA27AYkJcSXPXemW4XgnJ
1R+VkzaJxe+eNlXtk4z3NXFTBcNJihm5gWGb+cB0NMR1qWkcSRDC7bJu0OVAHG5L
B1qFTWIvplPzYwvvbAbGoNc/snzKj+jUMf+pb9F7IbMLZZfGq9u3aTXQ3wNN4bzn
4FOyjQCYMEMsMW32AXL9LUpArpzR1t4ORP2qlP+JFsv7D+pHsctLkFp4clVKWyZx
gdUtrSscxj4SxLgY/qS9C9gNjgpIdahPcr9GQHUC8T8qUPrHL954g1fcwW2qyrxc
0zVVe2u1UdAAblwV3yYHCeHKm7vAsWUuu024NTvL7XWYdv5qD/WhTeMT/BK1dS+q
kdxURmIvnqz852ZgW3hb2tOEdVHeHfNwXxb6nu4SKWmY566WCIuu+h7HKeojni+t
HWhV55biDvqStbgK1kXUCnYJQ5C0zsEkRWLTnLjZRw/EpZQM99TybQLZeeBbTgRQ
YQIDAQABo2kwZzAdBgNVHQ4EFgQUPOwbaFoz/x4caqAQUgmBJwSCjKYwEgYDVR0T
AQH/BAgwBgEB/wIBADAOBgNVHQ8BAf8EBAMCAuQwIgYDVR0RBBswGYIXaXN0aW9k
LmlzdGlvLXN5c3RlbS5zdmMwDQYJKoZIhvcNAQELBQADggIBAFpKbFho8RGcri1Z
lQxlsCjZ0ikLwd8YjFeCfhAe1GKsfxTM+LaNYT+5kS+xXwqnB31sT1JhEW9W1P6Q
SpNQiRkP4I56zHB2bBUQKQVrdjtPo1ApsnwX/wyjuSzvGzHkGlcZxdVaUPdaOll9
W553oUqwa0PHHQ7sbmx7+QyxLhptlwhUhH2+i8wQQYAfT1LHQQVXMcH6UnfFFTZb
1uNDCmvX34iRebTp9NlDyJMR5/Ga1oZ3WYSGpM8obpP+tYjAZNxR0eRCy9REOpqg
wGNNTVJIdjtDKfv28ToGKSqtCBwUfu7coGrGKO6fPBHBLkwuFMgw7xtsP0vGku9e
ITE3wkqDyWxwaFdyED+NHlVfcAp48HGDiGGl2z/CBYYpoeGAjN8W8lItKuP5pt+x
HtwV4Un/7FJaMscBsbXWiQ0suTbkeAKKwGGLKv2319v486E3ijUud/CjfGHFgOu7
+WmUM/NVy4NzfkQw+dykjkyZaFhcf4iOYqj/34ZyZPjZYOEPn7bi+55aIUmwLLXe
5QeaAmhkyxaOwuWuOx+qyMuAcREOxnrph4BkNQSyJmT2c4vJeate5uxw32pBHHUn
uGiJyvdUvJOl3UrIfOyxvj4xsmEmNJEz/H76CSFLEO2iCZaxl9F2FfLjVsnpNr53
Q90p9mudhwujd1H9zdYscxLOM/gg
-----END CERTIFICATE-----
`

	validWorkloadCert = `-----BEGIN CERTIFICATE-----
MIIFjTCCA3WgAwIBAgIUC+GrPcadhkCcPpgQK4f3v4WYlyEwDQYJKoZIhvcNAQEL
BQAwODEOMAwGA1UECgwFSXN0aW8xGDAWBgNVBAMMD0ludGVybWVkaWF0ZSBDQTEM
MAoGA1UEBwwDZm9vMB4XDTIwMDYyMDAyMDkwOFoXDTMwMDYxODAyMDkwOFowODEO
MAwGA1UECgwFSXN0aW8xGDAWBgNVBAMMD0ludGVybWVkaWF0ZSBDQTEMMAoGA1UE
BwwDZm9vMIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEAu9DKxpD8aaz7
z7ifo4eO0wOIAR9R6h25sI9GnBXYErxJYqFHq3Mr0b52K7o867bBWA/JI5XqqBiX
d+yq6S0/NCV14Fnsq9EvW0ZOmnr8jSPAChFSW2kXC3y7OHbeV+g47CvBsLG1gKzY
JDg2UHCmB10cODE9MmB1Dg8QdfhjQPMzCyvUG8V+rvUzIrrHDsFidKQFyU4HGcg9
JQoVsmmevQ4TGclRkjXwsN9Z+9p/uzEYNMFjgIooJTrKGMqkghgviXQwzSuCptL5
sxloYrp1GQYxl5U4iFpRXmbHBqZ5zyQJBxTICE5cpibJ8q7XYw8wwlXUE8240PW0
hB5HA6Z4RxCSOSi49NiAwTclILSQypheGXX5BoLyq6wLvG3hj+CXsy0Z5KQyYNvt
fXxc7EBcvqxFWiahhAsIagWZpqwfyPMMKgS4QLDncnbeya1TnSbo/V1zAnOghZqp
ph+VOt/fZZLns34NJa4tqcM+eIKaL39XqhnNeRMaQVa1hrSXFLP+GT6qdYLes20h
/YbSCYz4n2kbfZE3/5229p/IOabY1byAyDp6rwbylZjNQoImRpyCOoN1AFPf6jes
PaHc2IGRnwRNQsuEpwZZr8OMsmI80c5wCrC6c2tJO1SeUdq5M+j+fjuc5N2ETGr4
NOn9PVuUMzSC+zKxpdZ4RI/bBaLY3WkCAwEAAaOBjjCBizAdBgNVHQ4EFgQUcq4n
A6SfC1QYSQsTnfBrbNONzM8wDAYDVR0TAQH/BAIwADALBgNVHQ8EBAMCBaAwHQYD
VR0lBBYwFAYIKwYBBQUHAwEGCCsGAQUFBwMCMDAGA1UdEQQpMCeGJXNwaWZmZTov
L2Zvby5kb21haW4uY29tL25zL2Zvby9zYS9iYXIwDQYJKoZIhvcNAQELBQADggIB
AAqKWKuCMvxqhd0yhKVmrDHUMRiyVzZPah+hK8dNZfUaXhGjZY0pyqDQf2hDZ7os
/lYdIK+xVi8Hky86poYiBEjL8bwd06f8SIWAKRiVP7u1/nZ8VFV237n52PcbGxG0
6ksd8PWuR0zbErC5hXu8p0xkiszOAnzXHGDKk5VbrlDltY84lTukcO+v2kNplQC0
hBgGdusQGm3w09WGW2kHEhG3HcLXp68V6WrmAvQyS21v3r2FBFsNpoBJA75a0yN4
nNEXElIg2h2W4Yo8EOCxebz1+QlXX8J0cJb9NwHGUg14fssiaEd4086hR8pYdOMC
nZRO/FlyR7yKd3VhxFApzcfqN2iYijrKZIEIt0Iur9WFmWbkSdp3wzmyGcGJ46F4
IQPvSjiBDHE6nCIdRIXgcl7OXOoN1xcK2CEe+qZVioCEk5mtL4RMosSDPWgaMM2u
KR+cvnkwIBVsGKuKdvwXVoIzDTYaqsQudlqMeAVzbTJpzsm27IE2AyTjGsGNv++g
2dbqyUbAVJ+9GYuDcQ5J7bHcABBxrs5vZodmIuPBmDs9i72fQlpqE3tWLfbi17q0
PyCBneTwK2P/7ux8RSrP8QQkYYMcIowJ/Moe/7e6uOqfJt26C2uIm6m/NQbxShYD
SJnLQ77AQAqwF9+gnEx5eWfu/A00DOApCcDUeV/SYsA1
-----END CERTIFICATE-----
`

	validWorkloadKey = `-----BEGIN RSA PRIVATE KEY-----
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
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
			body:       validResponse,
			twoServers: false,
		},
		{
			name:       "success",
			template:   inputStringTemplate2,
			trustCert:  true,
			status:     http.StatusOK,
			body:       validResponse,
			twoServers: true,
		},
		{
			name:        "Invalid input 1",
			template:    "foo||URL1",
			trustCert:   false,
			status:      http.StatusOK,
			body:        validResponse,
			twoServers:  false,
			errContains: "config is invalid",
		},
		{
			name:        "Invalid input 2",
			template:    "foo|URL1|bar|URL2",
			trustCert:   false,
			status:      http.StatusOK,
			body:        validResponse,
			twoServers:  true,
			errContains: "config is invalid",
		},
		{
			name:        "Invalid input 3",
			template:    "URL1||bar|URL2",
			trustCert:   false,
			status:      http.StatusOK,
			body:        validResponse,
			twoServers:  true,
			errContains: "config is invalid",
		},
		{
			name:        "Unauthenticated cert",
			template:    inputStringTemplate1,
			trustCert:   false,
			status:      http.StatusOK,
			body:        validResponse,
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
			body:        invalidResponse,
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
func TestVerifyPeerCert(t *testing.T) {
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
						InsecureSkipVerify:    true,
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
				if !strings.Contains(err.Error(), testCase.errContains) {
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
