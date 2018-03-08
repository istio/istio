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

package util

import (
	"crypto/x509"
	"strings"
	"testing"
	"time"
)

var (
	key = `
-----BEGIN RSA PRIVATE KEY-----
MIIEogIBAAKCAQEAqO7HZN9K87yuMwmkteQqxkPLdZ6GVrVB1ualexDcslQl3kr3
2pGBjc9PugqiWeoZr5OOFXG6ME+ysHOXGadKl6rM1Lu3PjqNbjK9Y2wDf+iEnHEL
aQVI2Qu9yKFSXoFbSsnekw8CZAS8oXG9hQOMYTgYjwUsavaP54JaDbOB9eeSokQ2
e3Pz0w1zRmWSU0fn5ef8YOZNrAN14k2jSLACRvGv0IOr19yN/Z8BSV8/aTZ2Zx1a
nyPSxonWZlMRdcA+98MCumrJCV9qdJI7coDA00q2ryMIxfGy//F9fdxxKvftCZBw
Q+MIJMRqLapJ/NjKBYzWZa7k0HRTsOFfxnfFFQIDAQABAoIBAGxz7zrJR7s21Lcb
Z80GUJe8inBWd3RPJZert21MpAMwlqchhgGiDIRYJZ0Qmq4S5q6bkkoeGyRM5jD1
5Hmptu+rzZh9cuTWflnS5VdgztZdFlXBFUw1AlGlgg+90b2uWkenVecfaa+AgwE6
nis43fTEKLAY6C07YaOFQf8t0S9mkln2NV/nheiMgUI8NgH8OGbUnG37ACTWWMy0
JV3rEYa4gIk1RN3obMVok7DM4Yl5KPiMRO5oNNxv88f1hI8VIJZLOgyqYxNcolfR
st3OGcOmRZ5/lWXWZmJXkrP8xjiZQ2/uOh5GZa57gEQeZ/2K6y1L0kBbItbu6fHv
QNjcCDkCgYEAwrPg+s+6f/YFCGb3G62bECR/1fkig8Lmu9IG1RXIzyIe2agBhHpR
OVIedX95fxsFv4wJM737lxeZqpLMc3RNZULlAaf1VWMpWBzv/brNYcFxSClFLxU/
hll+QsrpRlRxAXG0ghaTO0euliHQIMDgEidD78+L2qhGetmWp+o4SwMCgYEA3h33
FChBwlQzpJ9dYGzhgg4EVPj+WGk2cTOADFVjNKRUn7Qc3tSu9SHAq5DYdebb2D/X
y++q+1FBiDOztmai1b4wl+VSOVY2t+lVVz0zhq5z0yG+oA+qFKX5zOBLfSpTfmSn
YloKvcGpRqT4mFy5ng50tkY5/tCi9NxxujVR6AcCgYArhf5S0sDD/gDeAfZXL5Ws
JByXfluizJy7e5WfaIE9HEl9Kjs8nAMwJxU7+sT0DtxYFzuvX1awTcxB/xLI9ESg
0DVVC3CiJ8qEMePL+kgTBCUIloEqpztOEw9Qav9+gz3Hrt1E/zrmU33JfcGCsNrl
8/UR1HlU5azrpVwyKP9wdQKBgDFLszdtC9MmPuPtXpr070Oe+sUlEcXra+LJzERw
evkG86USI0otJ7tNx2YMWo4oM2iWGr2vLmJikUm6N8tmkgMgF8bOZWZGRRSiG4em
FJZyh1A3cAg0EcpNX9hhez+HMkqd6ixA0Zt1rKz6FhYylhuHF84QXfS4t0Hi3va1
uLznAoGAO2PjqupdJGGWoaFXcBQolmxEN9L3LdyKeNYgAy/dpNKjjeWzctVEeSzi
z9o/ElSQJRimmBcQOVuEE9gt8tm2GYSOaKdGpZ6nMTwGnxDhovcnhxfKpi7ZpQ2V
api5/m4vGqH+CMn+/Qfos3JRaZt0B2grS8CDzhIUYnZ5gwEHT28=
-----END RSA PRIVATE KEY-----`

	keyMismatch = `
-----BEGIN RSA PRIVATE KEY-----
MIIEogIBAAKCAQEAu29J4OnIUE6cCZNMdGaamwFIqaVGJQSSMeq3MVs0kXWqcZ5j
aTCaisA9QP/rWbBFRJ91N2qnFyf9JYh+1xUi//xjML4FPFIvULFxJE09vaiaKLbb
uUYgq6cFu8Zxs9NvSCqrpMg7e6v2ctGbZp0qLf9Bljqf40NjHghVCtRLMdcE69md
74irIs3ZpjK58qNJ1FEGjjX1A2pFgyYq88bumxIEA7XCn3Spg2LlGKMq6hrGZ0Gp
lo5xUksP/4/eGgcP+Z8XprCIR+siv6tVUuLpLK2dVvy4QrFpgmS5PNiGCaCulW8c
padk1yaAnJUt0LDYhqo8Jli2wZU2mLKPpWGCHwIDAQABAoIBAAduI/mciupUE291
vWQn5R0b8et3t84j3j/IVDbKrMzPY1BZvQsgeB/j+wmmm4fUqwpLX/QwcCtE1I42
WQDhv59yO+RkxAReJa4HOrr7rOTl2CahCRjRJN5pIpsNBNjiGtk7h85ieakvafen
Q4fP1yo3ojv4WvpBY55/Kg/h1pFFKWfPgitJybCQVtJ5R10JnlV7ajHnREjLhNKs
M8JeTIMiQmmpqGrUR1FH7G0lVTsawgoK9fS8sUNf49lB3YiKOl46i124Gz/U1FKM
lD+rLDmISvjUyBV1IWPjF7YvdOILmv/FhyKl4t890nb9BEBRIPh508mgu5NrG6AS
0c4kYRECgYEA7wPQkHmZcVThL/KVQJxnZ6aPpllIw6hkVEUopRDV8fdwlno9juqx
GiGlSv2BAMNtwEto5DUt5S9Op2LCjYGi3adJhT182FSeoMFT4GUrMFe8qBTZL9Wd
zTE0EF/wU3DZeK4TcuGXBWs/MfZMRjNJEA8QCf7FL7IVdfDzY9kaDz0CgYEAyMEf
IH+YaqTuGlkl3Z3fUgI6BG/np/0JuXN+XgVsd0aJCQldMeJqqjo0Epw9t83+TZFm
3Ko7GmsAbTqv32857uCIQpHZ7ELoxdkz55oEl3TqNTJlqOt6mN3ak9jZtLcUXGUK
LpOuFGwIP2UaqsjSqvHoNiZXsgIH1Aya69nb7IsCgYBCa2j1/RSq7c92J49aWRxT
LXIV5BHYbV8UG/Pjiv4pM33SEz4wDQASJu9sG25R6/z/xvTrFewfGDpfQY6XDENa
HTbNE/0xkLJUMeVBIlwSHw+KFeEU7ePgNaAmPMLoLSAB7T3yWsZA90CkfbFMgMv4
7naikG3zhyV3lPHN+XLIcQKBgAmiQuUjWmQbwBVhm9CTx+i+lJwr5pkIKpRMt465
gegDaYYWffNr4ySCIIeYGdodN4vvY1lJjgaJhf6350K4qrYM7l0LdMLCvzrnXndJ
y9ic0rR0064UhtCZLOkVafUjKAX7D08G5T6zpH2uU2ZItttfOn6GvoSbVlbVuAWD
cetbAoGAcXrE+IBUcVbo08As8stENDgkTW5TCERpK33BSiO128ZwQQDK4uz76t7T
L5jnovP/yvauWK0i6SPh6tqpoCNyIp4JXh6cOF0Su7T06+hxdd+fpBARwMTgxS0Z
evPYztv1kb3ZJ4kCVwBVHia97sEveWezYYplyxQsP53au1Na6II=
-----END RSA PRIVATE KEY-----`

	keyBad = `
-----BEGIN RSA PRE KEY-----
MIIEowIBAAKCAQEAw/OBAAhDu58f0HkJlJBtb42Jp9EECC+WYEOVEdM/Y9fqcoSF
b19NxztVqy0r/aW8pCO3DZ2EYIA3Y9pYasDfhsIl9lhQkvEwk/05iL6oNrZ45Bgs
iSK+R5OlO9pXtj6HF948qFTDYbYVqki3rAWSSYeGpQ+/s/xcIIIKH5ozKs7DTqR8
svQ6t7Hxg0vYSUCHfJo25yIvoo8XGZxrFWOZDXfHHC22q8kuuxT82bdQo7KzYhgn
uujyzIZYqgG9BuUmB6UYdvuDRRDz4HDfERSFFxZbTAaMPNgCRvQnkPS0DJO0XZW2
T9m3bQvaqTgFI/capuhhgRcP0UrStJKZO7LVHQIDAQABAoIBAFLw0v2Mgf78j57S
XLfBmlDJfCbIVgiQ+/mrIYH2BLLiRZ5LcZ9+m5FlEBHwgNpQONTROT5OGiYun0No
vFwTX4nOy/rFzvUjmghJ+vxilxjxi6IgiVlSl2/8ksgO12mQdeYob0xg9IJ7bBgz
x2rMwOrWrqtXSzGH9AbehCJ0RowrUUjTujnow8WrDVS0cjPIl9c1eQDIDlHCUskC
iGMYYfJtB1nkdw1Kkp1YVmCYqwzVENi6+Hx66j/oOtGteTelwFmclc7JIK7liKEZ
xnDbgVTkIp9nSszpHStWikwh/srCWI7trC/k0viViZQLOd/64CyJ/sf8uZDzsG7f
hoiK3pECgYEAxF/d7QcDWaXCBkw4yC8ozqp6KTcA8thVuBK0Adtpzuf9t8h+B7V2
wlkSEs4A3YiUnxolEh1XOT+0u3uGfawlhFgXxEEHeBq+Lz6PIbWuVSl4PiWo8vtj
9MoBYRPtJelhHkBfjunqqaFwdRQQvXjmCsQfx4UAhBxdXvc2kTR/y08CgYEA/3K8
DKXldQliScqkG+Acj6nNgugecLYjCHgmAglHX6jwDuTVqNF3cv3DF5bZ4/nIbDsk
WooVhS4AEFYWceqmTveGsJNuDMoRSWNwDFRBu5Iq6LxneKiXp9vuZ4U4xZNejrgx
la7w1hQs92qCloY4Jxw9Ls3zKub4vC26CfJwzdMCgYBGw0T1ZNGQPGruWgkcGeJa
lpPuxiNRXyOEcTjscmRuaqrCzzybCokA/5fDrvgg3Fax/nndTTVhK9O0u457Os1K
I3RtBAHtBbYC0EhDnXR0u7zYqDl5VZ1vWFum38dVIgQdIpVMqn4lIkej6Ncfb7F1
r7bD7umAsbfzwKGpMYHbgQKBgQDL03vzR6hIc71mjffWekOv6lieXKJ1Yw+fIWeK
dmbqEH3EFJnbg5AhRBSYTPj9bICcw7AlQksbonHQlzB/ozEij2V8nZbRQ6b5fQuZ
+t0cUuxEGpkhcLzZ5qZbGbUMCaQIkzaVbiqjVyPuI6Ghg+VoZ6L2JsUh9XyBgqcQ
as/RmwKBgGPB8PHYHyz0km8LxM/GPstcoO4Ls5coS3MX2EBDKGqWOIOtLKz0azc7
R4beF5BJE6ulhLig4fkOWH4CIvw2Y1/22GJE/fYjUTRMD57ZdYuKqSyMNxwqiolw
xGSDfnFvR13RCqeUdlQofVYpolqrSobOyOVfQv2ksnPPsC87NISM
-----END RSA PRIVATE KEY-----`

	certChainBad = `
-----BEGIN CERTIATE-----
MIIC+zCCAeOgAwIBAgIQQ0vFSayWg4FQBBr1EpI5rzANBgkqhkiG9w0BAQsFADAT
MREwDwYDVQQKEwhKdWp1IG9yZzAeFw0xNzAzMTEwNjA0MDJaFw0xODAzMTEwNjA0
MDJaMBMxETAPBgNVBAoTCEp1anUgb3JnMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A
MIIBCgKCAQEAw/OBAAhDu58f0HkJlJBtb42Jp9EECC+WYEOVEdM/Y9fqcoSFb19N
xztVqy0r/aW8pCO3DZ2EYIA3Y9pYasDfhsIl9lhQkvEwk/05iL6oNrZ45BgsiSK+
R5OlO9pXtj6HF948qFTDYbYVqki3rAWSSYeGpQ+/s/xcIIIKH5ozKs7DTqR8svQ6
t7Hxg0vYSUCHfJo25yIvoo8XGZxrFWOZDXfHHC22q8kuuxT82bdQo7KzYhgnuujy
zIZYqgG9BuUmB6UYdvuDRRDz4HDfERSFFxZbTAaMPNgCRvQnkPS0DJO0XZW2T9m3
bQvaqTgFI/capuhhgRcP0UrStJKZO7LVHQIDAQABo0swSTAOBgNVHQ8BAf8EBAMC
BaAwEwYDVR0lBAwwCgYIKwYBBQUHAwEwDAYDVR0TAQH/BAIwADAUBgNVHREEDTAL
gglsb2NhbGhvc3QwDQYJKoZIhvcNAQELBQADggEBAITDuqOhN1jwiA72qSWzOwuy
bMHPkUTUw2JfICtPS0AlfNNVJXREUi4KoX81ju126PGQeOTApWvS5Kkd6PbNqVH9
g3myAKrkyjewTfFtK5OOOQGzQT6lCEhKdZJusdqfAMl1heFJGnZ6GAi38ftdz2Z8
0LPyyIaVBvexNnTPrqoBqdtWyzjYIdMnsSNWJnldmWjwA76sW+vvlLvTONiT4unM
8ia4GGIw7GK4E/7qxl27q6pXdZkZgG53XItYiUJGAKeBJ2nQfXq0qSmtpHkF17Cu
hw25X3FJpzRq62JxTx5q6+M2c07g4dkbfMDp/TO7vF4SWruU6JBZj5MVDYn4PEA=
-----END CERTIFICATE-----`

	certChainNoRoot = `
-----BEGIN CERTIFICATE-----
MIIC+zCCAeOgAwIBAgIQQ0vFSayWg4FQBBr1EpI5rzANBgkqhkiG9w0BAQsFADAT
MREwDwYDVQQKEwhKdWp1IG9yZzAeFw0xNzAzMTEwNjA0MDJaFw0xODAzMTEwNjA0
MDJaMBMxETAPBgNVBAoTCEp1anUgb3JnMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A
MIIBCgKCAQEAw/OBAAhDu58f0HkJlJBtb42Jp9EECC+WYEOVEdM/Y9fqcoSFb19N
xztVqy0r/aW8pCO3DZ2EYIA3Y9pYasDfhsIl9lhQkvEwk/05iL6oNrZ45BgsiSK+
R5OlO9pXtj6HF948qFTDYbYVqki3rAWSSYeGpQ+/s/xcIIIKH5ozKs7DTqR8svQ6
t7Hxg0vYSUCHfJo25yIvoo8XGZxrFWOZDXfHHC22q8kuuxT82bdQo7KzYhgnuujy
zIZYqgG9BuUmB6UYdvuDRRDz4HDfERSFFxZbTAaMPNgCRvQnkPS0DJO0XZW2T9m3
bQvaqTgFI/capuhhgRcP0UrStJKZO7LVHQIDAQABo0swSTAOBgNVHQ8BAf8EBAMC
BaAwEwYDVR0lBAwwCgYIKwYBBQUHAwEwDAYDVR0TAQH/BAIwADAUBgNVHREEDTAL
gglsb2NhbGhvc3QwDQYJKoZIhvcNAQELBQADggEBAITDuqOhN1jwiA72qSWzOwuy
bMHPkUTUw2JfICtPS0AlfNNVJXREUi4KoX81ju126PGQeOTApWvS5Kkd6PbNqVH9
g3myAKrkyjewTfFtK5OOOQGzQT6lCEhKdZJusdqfAMl1heFJGnZ6GAi38ftdz2Z8
0LPyyIaVBvexNnTPrqoBqdtWyzjYIdMnsSNWJnldmWjwA76sW+vvlLvTONiT4unM
8ia4GGIw7GK4E/7qxl27q6pXdZkZgG53XItYiUJGAKeBJ2nQfXq0qSmtpHkF17Cu
hw25X3FJpzRq62JxTx5q6+M2c07g4dkbfMDp/TO7vF4SWruU6JBZj5MVDYn4PEA=
-----END CERTIFICATE-----`

	certChain = `
-----BEGIN CERTIFICATE-----
MIIDKjCCAhKgAwIBAgIRALNe48yoHlrYlCaKQhjEjHwwDQYJKoZIhvcNAQELBQAw
HDEaMBgGA1UEChMRazhzLmNsdXN0ZXIubG9jYWwwHhcNMTgwMzA4MTM0OTU3WhcN
MTgwMzA5MDg0OTU3WjALMQkwBwYDVQQKEwAwggEiMA0GCSqGSIb3DQEBAQUAA4IB
DwAwggEKAoIBAQCo7sdk30rzvK4zCaS15CrGQ8t1noZWtUHW5qV7ENyyVCXeSvfa
kYGNz0+6CqJZ6hmvk44VcbowT7Kwc5cZp0qXqszUu7c+Oo1uMr1jbAN/6ISccQtp
BUjZC73IoVJegVtKyd6TDwJkBLyhcb2FA4xhOBiPBSxq9o/ngloNs4H155KiRDZ7
c/PTDXNGZZJTR+fl5/xg5k2sA3XiTaNIsAJG8a/Qg6vX3I39nwFJXz9pNnZnHVqf
I9LGidZmUxF1wD73wwK6askJX2p0kjtygMDTSravIwjF8bL/8X193HEq9+0JkHBD
4wgkxGotqkn82MoFjNZlruTQdFOw4V/Gd8UVAgMBAAGjeDB2MA4GA1UdDwEB/wQE
AwIFoDAdBgNVHSUEFjAUBggrBgEFBQcDAQYIKwYBBQUHAwIwDAYDVR0TAQH/BAIw
ADA3BgNVHREEMDAuhixzcGlmZmU6Ly9jbHVzdGVyLmxvY2FsL25zL2RlZmF1bHQv
c2EvZGVmYXVsdDANBgkqhkiG9w0BAQsFAAOCAQEACedVDtOjTHHX0fJK+xKHuTjT
HAPXVwQ6CLpW8hDvNIs9NF7f1mLqkfj6PTp/N3o11lxy4RxIkegqVfU+sq5in2eN
uEmOG2unYmacI8SDTxjJrZLgg1M40bP2xCKitmtD/dshxiJNiEZahHk6Raw4Odl2
okWN+TMcKU6QNRxdVJAoTFeqM/QuVR15tDDBdXJW2i0MT35oArkGfsOmx1oER+QE
XuWQmD2CRJo6ntwnhcldxDSndQAn0ODbFCJyrdCbzvoRj5rIics/SI6WWB8KTnYL
rYRXocKLYRDY4P4IRQJfOBDo59DlWT2+Q3dhDMKkJlzfpasJfjfqZpqFaxxD9A==
-----END CERTIFICATE-----`

	rootCertBad = `
-----BEGIN -----
MIIC5jCCAc6gAwIBAgIRAIDngVC9z3HRR4DdOvnKO38wDQYJKoZIhvcNAQELBQAw
HDEaMBgGA1UEChMRazhzLmNsdXN0ZXIubG9jYWwwHhcNMTcxMTE1MDAzMzUyWhcN
MjcxMTEzMDAzMzUyWjAcMRowGAYDVQQKExFrOHMuY2x1c3Rlci5sb2NhbDCCASIw
DQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAOLNvPT59LqfuJFZEkHNg5BABXqX
Yy0yu/t60lsd+Z43eTjEctnhyk45/4KE909wSVdzrq6jvlWCki/iHLkbnZ9Bfk0E
mGwP2TOjihOPWH9F6i8yO6GI5wqeQki7yiT/NozMo/vSNrso0Xa8WoQSN6svziP8
b9OeSIIMWIa8F1vD1EOvyHYlZHPMw/IJCqAxQef50FpVu2sB8t4FKeswyv0+Twh+
J75hB9OiDnM1G8Ex3An4G6KeUX8ptuJS6aLemuZrqOG6dsaG4HrC6OuIuxfyRbe2
zJyyHeOnGhozGVXS9TpCp3Mkr54NyKl4+p3XfeVtuBeG7UUvHS7EvS+2Bl0CAwEA
AaMjMCEwDgYDVR0PAQH/BAQDAgIEMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcN
AQELBQADggEBAEe3XmOAod4CoLkOWNFP6RbtSO3jDO6bzV0qOioS8Yj55eQ78hR9
R14TG5+QCHXz4W3FQMsgEg1OQodmw6lhupkvQn1ZP/zf3a/kfTuK0VOIzqeKe4TI
IgsccmELXGdxojN23311/tAcq3d8pSTKusH7KNwAQWmerkxB6wzSHTiJWICFJzs4
RWeVWm0l72yZcYFaZ/LBkn+gRyV88r51BR+IR7sMDB7k6hsdMWdxvNESr1h9JU+Q
NbOwbkIREzozcpaJ2eSiksLkPIxh8/zaULUpPbVMOeOIybUK4iW+K2FyibCc5r9d
vbw9mUuRBuYCROUaNv2/TAkauxVPCYPq7Ow=
-----END CERTIFICATE-----`

	rootCert = `
-----BEGIN CERTIFICATE-----
MIIC5TCCAc2gAwIBAgIQJtzUpEfsex4qLAl4lwRS8zANBgkqhkiG9w0BAQsFADAc
MRowGAYDVQQKExFrOHMuY2x1c3Rlci5sb2NhbDAeFw0xODAzMDcxODQ2NTRaFw0x
OTAzMDcxODQ2NTRaMBwxGjAYBgNVBAoTEWs4cy5jbHVzdGVyLmxvY2FsMIIBIjAN
BgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAtKApGulkupED+KY1+Eb02mfJ2jHT
iksSyMT3Fdg3Lh3HiVDqVTlgQoQMQg/Gv0J60hdinkbKmeqqhacLc29PGqhEykgU
vJwdAg1Raip/Tar6/MDQHxvUNWlYH70P7MSWMej8hc2IrqhoHoCcmMsqz5AWCiD0
Lu6LsFVilgaxkeIlg5HpJBjNt62doOmZNNjYiuHTk11VC4sDXyv5zWXt/CDmh14i
ej6Ufb+haP88n2fiMtLvbhLyLDe9RFgnmtheFvOe+8dPlhYLQc/7qAA5wZNs4ekP
gM1Haf6QE8+0kUaH4OMLm9ov/088a3l2hfMDszfdMLI74uCsd69AKMNg+QIDAQAB
oyMwITAOBgNVHQ8BAf8EBAMCAgQwDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0B
AQsFAAOCAQEAqj4GpBaUtC4RoapeY/Kz74T13awNU1ymoWZclOoUsHJgMZjSBgab
HnX1JBT91D3/YTTcWeaLkGG+KVNHiPa316kVmKrH6n2QPoW6Ps6TDmSWZGlyPr5t
CjV1LIxnHmsMcyGFFP6tFGNYrNvBsxjdV2cfSEHv+KjLnjw9DCCymCa3nkpazfWE
fBvIyGDYBoPtpGZ91r/aikFxt7bpr+u+N0Ykao2daaQQFHG2bGgi2tQAUyUaVaAK
ExheYWxAtzou4dEPPxyHcuvRQmkDG4poVpjzmEixfV0zYq2Ufz92zm1BFXYJIqzI
FQq776fWBWWMKRA2fMPSjBDDw8Ze/bgUPQ==
-----END CERTIFICATE-----`

	notBefore = &VerifyFields{
		NotBefore: time.Unix(0, 0),
	}

	ttl = &VerifyFields{
		TTL: time.Duration(0),
	}

	extKeyUsage = &VerifyFields{
		TTL: time.Duration(1),
	}

	keyUsage = &VerifyFields{
		ExtKeyUsage: []x509.ExtKeyUsage{1, 2},
		KeyUsage:    2,
	}

	isCA = &VerifyFields{
		ExtKeyUsage: []x509.ExtKeyUsage{1, 2},
		KeyUsage:    5,
		IsCA:        true,
	}

	org = &VerifyFields{
		ExtKeyUsage: []x509.ExtKeyUsage{1, 2},
		KeyUsage:    5,
		Org:         "bad",
	}

	success = &VerifyFields{
		ExtKeyUsage: []x509.ExtKeyUsage{1, 2},
		KeyUsage:    5,
	}
)

func TestVerifyCert(t *testing.T) {
	testCases := map[string]struct {
		privPem        []byte
		certChainPem   []byte
		rootCertPem    []byte
		host           string
		expectedFields *VerifyFields
		expectedErr    string
	}{
		"Root cert bad": {
			privPem:        nil,
			certChainPem:   nil,
			rootCertPem:    []byte(rootCertBad),
			host:           "",
			expectedFields: nil,
			expectedErr:    "failed to parse root certificate",
		},
		"Cert chain bad": {
			privPem:        nil,
			certChainPem:   []byte(certChainBad),
			rootCertPem:    []byte(rootCert),
			host:           "",
			expectedFields: nil,
			expectedErr:    "failed to parse certificate chain",
		},
		"Failed to verify cert chain": {
			privPem:        nil,
			certChainPem:   []byte(certChainNoRoot),
			rootCertPem:    []byte(rootCert),
			host:           "spiffe",
			expectedFields: nil,
			expectedErr:    "failed to verify certificate: x509:",
		},
		"Failed to verify key": {
			privPem:        []byte(keyBad),
			certChainPem:   []byte(certChain),
			rootCertPem:    []byte(rootCert),
			host:           "spiffe",
			expectedFields: nil,
			expectedErr:    "invalid PEM-encoded key",
		},
		"Failed to match key/cert": {
			privPem:        []byte(keyMismatch),
			certChainPem:   []byte(certChain),
			rootCertPem:    []byte(rootCert),
			host:           "spiffe",
			expectedFields: nil,
			expectedErr:    "the generated private key and cert doesn't match",
		},
		"Wrong SAN": {
			privPem:        []byte(key),
			certChainPem:   []byte(certChain),
			rootCertPem:    []byte(rootCert),
			host:           "spiffe",
			expectedFields: nil,
			expectedErr:    "the certificate doesn't have the expected SAN for: spiffe",
		},
		"Timestamp error": {
			privPem:        []byte(key),
			certChainPem:   []byte(certChain),
			rootCertPem:    []byte(rootCert),
			host:           "spiffe://cluster.local/ns/default/sa/default",
			expectedFields: notBefore,
			expectedErr:    "unexpected value for 'NotBefore' field",
		},
		"TTL error": {
			privPem:        []byte(key),
			certChainPem:   []byte(certChain),
			rootCertPem:    []byte(rootCert),
			host:           "spiffe://cluster.local/ns/default/sa/default",
			expectedFields: extKeyUsage,
			expectedErr:    "unexpected value for 'NotAfter' - 'NotBefore'",
		},
		"extKeyUsage error": {
			privPem:        []byte(key),
			certChainPem:   []byte(certChain),
			rootCertPem:    []byte(rootCert),
			host:           "spiffe://cluster.local/ns/default/sa/default",
			expectedFields: ttl,
			expectedErr:    "unexpected value for 'ExtKeyUsage' field",
		},
		"KeyUsage Error": {
			privPem:        []byte(key),
			certChainPem:   []byte(certChain),
			rootCertPem:    []byte(rootCert),
			host:           "spiffe://cluster.local/ns/default/sa/default",
			expectedFields: keyUsage,
			expectedErr:    "unexpected value for 'KeyUsage' field",
		},
		"IsCA error": {
			privPem:        []byte(key),
			certChainPem:   []byte(certChain),
			rootCertPem:    []byte(rootCert),
			host:           "spiffe://cluster.local/ns/default/sa/default",
			expectedFields: isCA,
			expectedErr:    "unexpected value for 'IsCA' field",
		},
		"Org error": {
			privPem:        []byte(key),
			certChainPem:   []byte(certChain),
			rootCertPem:    []byte(rootCert),
			host:           "spiffe://cluster.local/ns/default/sa/default",
			expectedFields: org,
			expectedErr:    "unexpected value for 'Organization' field",
		},
		"Succeeded": {
			privPem:        []byte(key),
			certChainPem:   []byte(certChain),
			rootCertPem:    []byte(rootCert),
			host:           "spiffe://cluster.local/ns/default/sa/default",
			expectedFields: success,
			expectedErr:    "",
		},
	}
	for id, tc := range testCases {
		err := VerifyCertificate(
			tc.privPem, tc.certChainPem, tc.rootCertPem, tc.host, tc.expectedFields)
		if err != nil {
			if len(tc.expectedErr) == 0 {
				t.Errorf("%s: Unexpected error: %v", id, err)
			} else if strings.Contains(err.Error(), tc.expectedErr) != true {
				t.Errorf("%s: Unexpected error: %v VS (expected) %s", id, err, tc.expectedErr)
			}
		} else if len(tc.expectedErr) != 0 {
			t.Errorf("%s: Expected error %s but succeeded", id, tc.expectedErr)
		}
	}
}
