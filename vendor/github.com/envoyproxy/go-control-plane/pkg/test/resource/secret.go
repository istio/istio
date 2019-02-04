// Copyright 2019 Envoyproxy Authors
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

package resource

import (
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
)

const (
	tlsName = "tlssecret"

	rootName = "rootsecret"

	privateKey = `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAsU265Ev3yVUAGds4qorN/QH0acOPO/gO7diGcfZ7QSQvjBUn
TGO00EquFyqU9ameU0X1YPUWMsATE7HEQZFV7dtiQdN7OATNMIWgAH4Rhl6zLbc0
sh2MpxJQqakE6klZo5ZDRuXm11o8tdM9+kj2m9SlwtrflXd+jQ1afvsgvUP1FsA5
rZfOk0+WBzWymJDEsSZELQVh6LM94Y/0x55vj0dx+jtweGpsBAbLwS9SWqk1ynKr
6K7ZYiUnfM1jTqIF4NTwfe79lfttXZZHLBjtm2UZQZWz4R1VRQP/yoFVWXUMydhg
fUsnBYoWBCPgLRDGdf6+/NmIcMOUdhAFQmd5YQIDAQABAoIBAD3bMGiVWE0VKoPa
x1o4MsUh+XMslrwFPrAb6ku4Aignx67Hcn5kCqDgbPwIDw/lrSbAMWsyFhx+hilI
y39UhPYGo7DzZvmUM0HKXJfPY63NPBWm5Ot/A6MF8L5ACUbzcCJyOeZyLqbTBHsq
x2SaL+8NsQbZ9Ubf+XacQgYq9rEQf4ZnbVBiM7Wx1qQikApkbA6Ik7d8f4ojSdgq
jbo+0JB51gspCfks0udi23WHaxW8vZO5EI+IWsiVk8Xd+TOotluvwgEjMN8ymJui
Z/Xcu45RAMgrW4jRuO8ePiIvTIvAiTTktz6pCVXiQhd3iX+GL3eL1ZbbYQnAY1/S
dENtpwECgYEAylK4je4Yf58xygkfaqGCjv99sNXGQ2GLvZQFUUen1fbWRCZN1Hot
wYXsF0M+J15ErwORAb8/E7roWcRHYJa8SjGNCBSyunCTH8LDVasKoICvdLiGfCI5
7oweQRKUOBN9yxD3rcIIH+8TzNOoeZA5g0Wk/vzszWINTbOXnTRX30UCgYEA4Fe/
bu4Uq6m1ZUZb7BjdOwkHrVD5WknayDECEDAaTuZBGEY71rpxjttgALbg29XJ4tpV
hjyBZF3IlBYyifSbRnlKFV3kGY604+2lOIhZKx86Crmmu+io+t3mfgq4daXXfgie
/qbWBGmin/dn3PMDhdtPCIYYLMIFfcFUiA5l1W0CgYEAnfOOmV90SM4jtLMCj+Cf
aLwViGSccCZLTimtLRNf+C7IgFPXFzZ7WkYPVunsMBfsTyXdoxuHRwP4OXx+rO2A
+ftNOy3NirgwY+9NSChMF5nfYKReebLOv2kshWjXxh+RaWNJuaFtbmDbeGEVejIa
dF1+voL+7CjMcgjvKI+gunECgYAPqTB87vPUc/lsw3ehSK8Q8vVtPOzbR7KVLQ6m
0KTVgy9iIW0F9Wf+AAR4qEuULR11z6YOw+SIfB+HbvFCPigkyEzKpw5IVnT8QFe7
VZFb+EcV/pXMIlbBhIIVpGvyEoyziKiIwF7KWhF3N08x1mkVjBS9VJcVcMnvWHKt
OMwVFQKBgDJQ5OmU6JtN+FZb4sh6vJ1zaqr8TQ5axF+0C9p7HEgnrsbLel78H0iQ
W4TS0TMSQ8pTybUXb+u9nTh4/dfuf7vJtS2N31/68XQO1sHC/hxZg8ow2Yeq//5p
Jt/f/Fu1KusLb/sM9FurtVKuVhu2qeAFQXat0uKop76fZV56XVrh
-----END RSA PRIVATE KEY-----`

	privateChain = `-----BEGIN CERTIFICATE-----
MIIDWzCCAkOgAwIBAgIQcojbQweHMt1XQLs8eIpyqTANBgkqhkiG9w0BAQsFADBF
MQswCQYDVQQGEwJBVTETMBEGA1UECAwKU29tZS1TdGF0ZTEhMB8GA1UECgwYSW50
ZXJuZXQgV2lkZ2l0cyBQdHkgTHRkMB4XDTE4MDgwOTIzMDg0MloXDTE5MDgwOTIz
MDg0MlowEzERMA8GA1UEChMISnVqdSBvcmcwggEiMA0GCSqGSIb3DQEBAQUAA4IB
DwAwggEKAoIBAQCxTbrkS/fJVQAZ2ziqis39AfRpw487+A7t2IZx9ntBJC+MFSdM
Y7TQSq4XKpT1qZ5TRfVg9RYywBMTscRBkVXt22JB03s4BM0whaAAfhGGXrMttzSy
HYynElCpqQTqSVmjlkNG5ebXWjy10z36SPab1KXC2t+Vd36NDVp++yC9Q/UWwDmt
l86TT5YHNbKYkMSxJkQtBWHosz3hj/THnm+PR3H6O3B4amwEBsvBL1JaqTXKcqvo
rtliJSd8zWNOogXg1PB97v2V+21dlkcsGO2bZRlBlbPhHVVFA//KgVVZdQzJ2GB9
SycFihYEI+AtEMZ1/r782Yhww5R2EAVCZ3lhAgMBAAGjeTB3MA4GA1UdDwEB/wQE
AwIFoDAMBgNVHRMBAf8EAjAAMB8GA1UdIwQYMBaAFCudfB/DxIfxp92elExpHJfN
eMliMDYGA1UdEQQvMC2GK3NwaWZmZTovL2NsdXN0ZXIubG9jYWwvbnMvZGVmYXVs
dC9zYS9zZXJ2ZXIwDQYJKoZIhvcNAQELBQADggEBAKQuLmnWXotaiA8SiV7QYWWw
aAHnlNS/HgvUmgNYort+IlhCb0a4098ncmnP0fyyuQloElEWKE/00/d9eLiocWHt
tKhftYQ6Z9DY5nNerbb3uv8fKrV91z5paFiHfXotw2e1zmtjP3Guve9agmKjhote
gcgPQ2MIfvmFDD7lGrkstDhubUwXXh4nE9bqTl6W2Jl/PIs6e/bvZtaUM/yySFbB
upmaQWrVXaWsAf5xI3PVtCQqCGOesI6YgDt3/SkiSMPsFb6dC5GKq/dVLGHw26Tw
jA25kE/wpOv0TvEAtEJ1V3GiDAODHwEt2m+IA58lZJcuj5GCqyHcKVW8KV8VVFc=
-----END CERTIFICATE-----`

	rootCert = `-----BEGIN CERTIFICATE-----
MIIDXTCCAkWgAwIBAgIJAOtgaSEVTdk7MA0GCSqGSIb3DQEBCwUAMEUxCzAJBgNV
BAYTAkFVMRMwEQYDVQQIDApTb21lLVN0YXRlMSEwHwYDVQQKDBhJbnRlcm5ldCBX
aWRnaXRzIFB0eSBMdGQwHhcNMTgwODA5MjMwODI3WhcNMjMwODA4MjMwODI3WjBF
MQswCQYDVQQGEwJBVTETMBEGA1UECAwKU29tZS1TdGF0ZTEhMB8GA1UECgwYSW50
ZXJuZXQgV2lkZ2l0cyBQdHkgTHRkMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIB
CgKCAQEAq0+Cg1WgcmC0X7Zcw75V/FZ9zJVhZinSAozxF5g8ci/QkGt0u1hI2lFg
UYNHnWi8bhxcIHpK56WkY1D/2lFmXc32YxlFhAy+Ox1FTIqOW/VnvteUnUoe8GUw
ADBEgtcZGUrAEKO8l8mjGyIpWbUI+G7tizB+9bx2dPuVXuzP/6ZZv5i1wmWhC/vp
CgV8VaJ0qAjUAnQ25Q9GETHqYxUDOq1f7LrSCX16yhfXsqXhKtNxF370VcvZWjTM
sQPn588QvSOdLYuYYnYz+TK4ixmCvoQtnzcmnhYSt3ae7YQW8vyD5huLtXl6jiU5
KKGm2cLdfpY2KEQKLpJQLum1TscGkQIDAQABo1AwTjAdBgNVHQ4EFgQUK518H8PE
h/Gn3Z6UTGkcl814yWIwHwYDVR0jBBgwFoAUK518H8PEh/Gn3Z6UTGkcl814yWIw
DAYDVR0TBAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAZ+x0ppSUYQUw6FCVFjkp
C99bX5O/jxRXoPCmsqh+NoOjvni65IKR7ilxDXLcrXaSNovopWDo4Fz7fQaR9xwk
0fIgu+JNdq72LmHlrCtjlD0Es08R+KUHWgpbHxaKq77+Ie/9w+pY3uDQz0dMy/yN
yw3pH4By0lYwKt4QzRtA9btCiLYivReV9b+3P2P5IR+BLtLXt4j8LvstoJYhagUn
L1qakOGqxFLL0P3CuulGACAH0jRRIhrREKnj3rVFGtdl4APafBn9XnJwzlwwj/GU
y5E2qAmEqYO6m1rVPeeE0kjP5rIOEH5qQKpxtJ2/gAqc0OOmbFrnbfOWGUNkWn7X
yA==
-----END CERTIFICATE-----`
)

// MakeSecrets generates an SDS secret
func MakeSecrets(tlsName, rootName string) []cache.Resource {
	return []cache.Resource{
		&auth.Secret{
			Name: tlsName,
			Type: &auth.Secret_TlsCertificate{
				TlsCertificate: &auth.TlsCertificate{
					PrivateKey: &core.DataSource{
						Specifier: &core.DataSource_InlineBytes{InlineBytes: []byte(privateKey)},
					},
					CertificateChain: &core.DataSource{
						Specifier: &core.DataSource_InlineBytes{InlineBytes: []byte(privateChain)},
					},
				},
			},
		},
		&auth.Secret{
			Name: rootName,
			Type: &auth.Secret_ValidationContext{
				ValidationContext: &auth.CertificateValidationContext{
					TrustedCa: &core.DataSource{
						Specifier: &core.DataSource_InlineBytes{InlineBytes: []byte(rootCert)},
					},
				},
			},
		},
	}
}
