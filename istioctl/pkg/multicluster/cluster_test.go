// Copyright Istio Authors.
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

package multicluster

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/clientcmd/api"

	"istio.io/istio/pkg/kube/secretcontroller"
)

func makeNamespace(name string) *v1.Namespace {
	return &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

var (
	kubeSystemNamespaceUID = types.UID("54643f96-eca0-11e9-bb97-42010a80000a")
	kubeSystemNamespace    = &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kube-system",
			UID:  kubeSystemNamespaceUID,
		},
	}
)

func TestClusterUID(t *testing.T) {
	client := fake.NewSimpleClientset()
	if _, err := clusterUID(client); err == nil {
		t.Errorf("clusterUID should fail when kube-system namespace is missing")
	}

	want := kubeSystemNamespaceUID
	client = fake.NewSimpleClientset(kubeSystemNamespace)
	got, err := clusterUID(client)
	if err != nil {
		t.Fatalf("clusterUID failed: %v", err)
	}
	if got != want {
		t.Fatalf("got %v want %v", got, want)
	}
}

var (
	goodClusterDesc = ClusterDesc{
		Network:              testNetwork,
		Namespace:            testNamespace,
		ServiceAccountReader: testServiceAccountName,
	}
	clusterDescFieldsNotSet = ClusterDesc{
		Network: testNetwork,
	}
	clusterDescWithDefaults = ClusterDesc{
		Network:              testNetwork,
		ServiceAccountReader: DefaultServiceAccountName,
		Namespace:            defaultIstioNamespace,
	}
)

func TestNewCluster(t *testing.T) {
	cases := []struct {
		name                    string
		objs                    []runtime.Object
		context                 string
		desc                    ClusterDesc
		injectClientCreateError error
		want                    *Cluster
		wantErrStr              string
	}{
		{
			name: "missing defaults",
			objs: []runtime.Object{
				kubeSystemNamespace,
				makeNamespace(defaultIstioNamespace),
			},
			context: testContext,
			desc:    clusterDescFieldsNotSet,
			want: &Cluster{
				ClusterDesc: clusterDescWithDefaults,
				Context:     testContext,
				clusterName: string(kubeSystemNamespaceUID),
				installed:   true,
			},
		},
		{
			name: "create client failure",
			objs: []runtime.Object{
				kubeSystemNamespace,
				makeNamespace(goodClusterDesc.Namespace),
			},
			injectClientCreateError: errors.New("failed to create client"),
			context:                 testContext,
			desc:                    goodClusterDesc,
			wantErrStr:              "failed to create client",
		},
		{
			name: "clusterName lookup failed",
			objs: []runtime.Object{
				makeNamespace(goodClusterDesc.Namespace),
			},
			context:    testContext,
			desc:       goodClusterDesc,
			wantErrStr: `namespaces "kube-system" not found`,
		},
		{
			name: "istio not installed",
			objs: []runtime.Object{
				kubeSystemNamespace,
			},
			context: testContext,
			desc:    goodClusterDesc,
			want: &Cluster{
				ClusterDesc: goodClusterDesc,
				Context:     testContext,
				clusterName: string(kubeSystemNamespaceUID),
				installed:   false,
			},
		},
		{
			name: "success",
			objs: []runtime.Object{
				kubeSystemNamespace,
				makeNamespace(goodClusterDesc.Namespace),
			},
			context: testContext,
			desc:    goodClusterDesc,
			want: &Cluster{
				ClusterDesc: goodClusterDesc,
				Context:     testContext,
				clusterName: string(kubeSystemNamespaceUID),
				installed:   true,
			},
		},
	}

	for i := range cases {
		c := &cases[i]
		t.Run(fmt.Sprintf("[%v] %v", i, c.name), func(tt *testing.T) {
			env := newFakeEnvironmentOrDie(tt, api.NewConfig(), c.objs...)
			env.injectClientCreateError = c.injectClientCreateError

			got, err := NewCluster(c.context, c.desc, env)
			if c.wantErrStr != "" {
				if err == nil {
					tt.Fatalf("wanted error including %q but got none", c.wantErrStr)
				} else if !strings.Contains(err.Error(), c.wantErrStr) {
					tt.Fatalf("wanted error including %q but got %v", c.wantErrStr, err)
				}
			} else if c.wantErrStr == "" && err != nil {
				tt.Fatalf("wanted non-error but got %q", err)
			} else {
				c.want.client = env.client
				if !reflect.DeepEqual(got, c.want) {
					tt.Fatalf("\n got %#v\nwant %#v", got, c.want)
				}
			}
		})
	}
}

func createTestClusterAndEnvOrDie(t *testing.T,
	context string,
	config *api.Config,
	desc ClusterDesc,
	objs ...runtime.Object,
) (*fakeEnvironment, *Cluster) {
	t.Helper()

	env := newFakeEnvironmentOrDie(t, config, objs...)
	c, err := NewCluster(context, desc, env)
	if err != nil {
		t.Fatalf("could not create test cluster: %v", err)
	}
	return env, c
}

var (
	testSecretLabeled0 = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testSecretLabeled0",
			Namespace: defaultIstioNamespace,
			Labels:    map[string]string{secretcontroller.MultiClusterSecretLabel: "true"},
		},
	}
	testSecretLabeled1 = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testSecretLabeled1",
			Namespace: defaultIstioNamespace,
			Labels:    map[string]string{secretcontroller.MultiClusterSecretLabel: "true"},
		},
	}
	testSecretNotLabeled0 = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testSecretNotLabeld0",
			Namespace: defaultIstioNamespace,
		},
	}
)

func TestReadRemoteSecrets(t *testing.T) {
	cases := []struct {
		name              string
		objs              []runtime.Object
		context           string
		desc              ClusterDesc
		injectListFailure error
		want              remoteSecrets
	}{
		{
			name: "list failed",
			objs: []runtime.Object{
				kubeSystemNamespace,
				makeNamespace(defaultIstioNamespace),
				testSecretLabeled0,
				testSecretLabeled1,
				testSecretNotLabeled0,
			},
			injectListFailure: errors.New("list failed"),
			context:           testContext,
			desc:              clusterDescWithDefaults,
			want:              remoteSecrets{},
		},
		{
			name: "no labeled secrets",
			objs: []runtime.Object{
				kubeSystemNamespace,
				makeNamespace(defaultIstioNamespace),
				testSecretNotLabeled0,
			},
			context: testContext,
			desc:    clusterDescWithDefaults,
			want:    remoteSecrets{},
		},
		{
			name: "success",
			objs: []runtime.Object{
				kubeSystemNamespace,
				makeNamespace(defaultIstioNamespace),
				testSecretLabeled0,
				testSecretLabeled1,
				testSecretNotLabeled0,
			},
			context: testContext,
			desc:    clusterDescWithDefaults,
			want: remoteSecrets{
				testSecretLabeled0.Name: testSecretLabeled0,
				testSecretLabeled1.Name: testSecretLabeled1,
			},
		},
	}

	for i := range cases {
		c := &cases[i]
		t.Run(fmt.Sprintf("[%v] %v", i, c.name), func(tt *testing.T) {
			g := NewWithT(tt)

			env, cluster := createTestClusterAndEnvOrDie(tt, c.context, nil, c.desc, c.objs...)
			if c.injectListFailure != nil {
				reaction := func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, c.injectListFailure
				}
				env.client.PrependReactor("list", "secrets", reaction)
			}
			got := cluster.readRemoteSecrets(env)
			g.Expect(got).To(Equal(c.want))
		})
	}
}

// cd samples/certs
// make root-ca
// make intermediate-ca
//
// # rootCertPEM
// cat root-root-cert.pem
//
// # caCertPEM
// cat intermediate/ca-cert.pem
var (
	rootCertPEM = []byte(`-----BEGIN CERTIFICATE-----
MIIFFDCCAvygAwIBAgIUNvN+0FmOtgNTyjKK730i4JgPDeMwDQYJKoZIhvcNAQEL
BQAwIjEOMAwGA1UECgwFSXN0aW8xEDAOBgNVBAMMB1Jvb3QgQ0EwHhcNMTkxMDI4
MTkzMjQxWhcNMjkxMDI1MTkzMjQxWjAiMQ4wDAYDVQQKDAVJc3RpbzEQMA4GA1UE
AwwHUm9vdCBDQTCCAiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBAMdaPD8I
ft/fFkqdLiBBQPTLhozaeGkEkGhBsXkoHw38CCdeaRekGOoZI58Ce/iOjCIACEZo
n1Y6SKYnl4FqPFjOy3uF0ZFMeCt8GA+QrlPdEfkDAnj3FFc+C1THov0R+FCv2Qrs
FR0I6OZen+CVWS53xQNagxfBi9XeFI833gKr8Qiv0WOJKuoTY3abw8FJKyPPHX4O
RnqLDwEr8BRQyqgWCQPGGL+quGV22dfI8tVxmj0lXR3fJs3kuNagCoCSSnpJSjGi
eYy80i+esUO0RCNLoA78ia9bua5juyU6sUZca7Yk28cbz29niaT79iB02vQI+U8x
DL11eq7Wg6zsUhTrzIwJKCMyhsQCEYmrYIfv3STqkxzdiePnyjGXorX3mVZsNAxT
fwerb5rGdNm8QVa+LgRMPZmLlRiMjGkut3O0S76bPthbp7dgAiYXfmHlucmrCs80
E8qpPpceZUqEHbK9IUmHeecvl3oSx2H7ym4id1dq55eyQXk7DiZbo0yciGsIhytF
PLXDpeop4r67vZfsn9EqWed8XH7PBGdZMkDH5gMp1OaO/gHmIf/qnCYB6wJOKk7z
+Dol7sggwy+KwU+gjINbJwvFX/3pwY5cIza4Ds2B7oUe4hohZo2HzCd+RaTPNM03
DDyQmKcLhwt3K5oZFVceK6rCOpWzMq7upXPVAgMBAAGjQjBAMB0GA1UdDgQWBBQ3
3/NM2B73DIxoRC4zlw9YP0kP0DAPBgNVHRMBAf8EBTADAQH/MA4GA1UdDwEB/wQE
AwIC5DANBgkqhkiG9w0BAQsFAAOCAgEAxbDDFmN84kPtl0dmAPbHqZRNkZkcZQ2n
M0i7ABVpaj9FH1zG4HeLxKyihfdHpF4N6aR95tmyzJd2OKSC1CiPDF85Lgc/+OdO
U2NRijl3wzcl2yqza1ChQK/clKkKFn4+WQgzBJbtiOmqD8NojJlw3juKclK25SAH
94bCksJg2Z834lsQY9cDIzqEackt/1NAa1IboZTQsJXzLZ9jAxv3TJWGapG7qHc5
5ojcm4h2WbDXoKWCBSU8Z2rFjT48x3YONjwWB7BPUdEOTwdbtpLoTDRFhUZttM9U
ovpsLTMumzUqmaI+2Q0gPVjQo4wvBPeouhEc2KYlvD6U7BrVz2JqEgSmsdJR4wNJ
sBf2kBuqdGiWCbDuGeJBGDc48jAmqvKdaljkt4IFigYuRUx8NFgvichbkpU/ZfuQ
CVpValVTXe7GMJadMnXLsoXMU1z57dbEdarej6TiCymOeIJ9oJF0g9ppNqq6NRnL
Y7pH4lN1U8lHxa52uPZ5HNsld3+fFKNq1tgbNhQ1Q9gn7nLalTsAr4RZJN9QMnse
k4OycvyY2i1iKYl5kcI2g38FzlIlALOrxd8nhQDBF5rRktfqp7t3HtKZubjkBwMQ
tP+N2C0otdj8D6IDHlT8OFr69n+PD4qR6P4bKxnjiYtEAqRvPlR96yrtjbdg/QgJ
0+aVGEMeDqg=
-----END CERTIFICATE-----`)

	caCertPEM = []byte(`-----BEGIN CERTIFICATE-----
MIIFoTCCA4mgAwIBAgICKY0wDQYJKoZIhvcNAQELBQAwIjEOMAwGA1UECgwFSXN0
aW8xEDAOBgNVBAMMB1Jvb3QgQ0EwHhcNMTkxMDI4MTkzMjU0WhcNMjExMDI3MTkz
MjU0WjBBMQ4wDAYDVQQKDAVJc3RpbzEYMBYGA1UEAwwPSW50ZXJtZWRpYXRlIENB
MRUwEwYDVQQHDAxpbnRlcm1lZGlhdGUwggIiMA0GCSqGSIb3DQEBAQUAA4ICDwAw
ggIKAoICAQCyiO1rIxJNqPcccdjMS0WunwzvKCD+CeoPtomhigHjVtuiXUVO5shc
uXmjj1uTn4WzUHl/TJI1Xid+y0Xr2dvU2fIx4uJ7Z8jj2kbbSCGBZu/JdjIGEQNs
C5dpY2d2xymduTNWQ1v2QKX3HwBMoKL0ob9c3tNvbQlkX/JPGMys0Y4NgHsp06XB
XP6louWKBxri+Kgu9yZVfCOvEyVdjD1AHV8L5rug6kcHnwwyZc0b1lK8mGLzOVs5
oLOG0APXW8psnM9KF/mgtBddAirtaCz1uf6MGtwpECOQl0IAk6UknqNoX7RPREnh
Ovjj2uqvDuhJ/7weVeTyg4eEYAwjKi24vNhM4RodTDAp8FZJ+O78yRoYANQB3dx5
yo9TXZ5QfJ+Q0JW5sgU0yP1zmmgzO6R+/ihy14egkIoNYWWDGS2gL5ylO5Q4iDGz
ifBK60cB6iIOq/sZNqJLxJyQ1gy+7N6C3YdN0OY0SYqXQ1QRpshlUpg5yMjvCIpg
lBqt2AkdLU6Fhdzgkd/7+p6jWFBqbQdtAtUwZwWANrDqU9TL/sDHJNv24FKDHNcg
H4Nuoj7qCylHD5N6n3XtpL6MkcSFlKeKA6Zm0e0y7zDzdLWf/TEpa8S+nwwUwO/8
MgkpxLvZzIaBi1YgUQFWdDS11ZiSG2WqI93tblD7zpcmZ1g08mbdyQIDAQABo4HB
MIG+MB0GA1UdDgQWBBQ+Y2cHzPB1Klr5RT6NF2npJ94xPDASBgNVHRMBAf8ECDAG
AQH/AgEAMA4GA1UdDwEB/wQEAwIC5DB5BgNVHREEcjBwhjFzcGlmZmU6Ly9jbHVz
dGVyLmxvY2FsL25zL2lzdGlvLXN5c3RlbS9zYS9jaXRhZGVshjBzcGlmZmU6Ly9p
bnRlcm1lZGlhdGUvbnMvaXN0aW8tc3lzdGVtL3NhL2NpdGFkZWyCCWxvY2FsaG9z
dDANBgkqhkiG9w0BAQsFAAOCAgEAs59zXiP+40iu2CldWqzczzA8au//TK5erbKt
nyxFcugSCl1zjqxE/eXO7hHYsVWUX9J1M0BbgEhAY7cFMXwdLpWzdepgm4a+QkUJ
KkmyZAEX0vqPHinuOzKtUZ7c8HgZft3i7ceuPU1S/tHd4XHYTL3Yf8ZrTvQ2o+Oa
GBiG0hJnaklonarbt9M2cIsHjb1BSS8bhrlhwEc3lqM+IDdNrBTPPUhI8sEln5ew
+g5R4s3hwtpimvkxCdigUJ9yL/Seef9XnAIRL1E6OT3ZDKtt4GvAikMKSjW5lssh
qJCQKhmd4JYuvIjxlKrJsgzEqb/oA47nD7+9P+kjc4w6cP5/XZMiT26o9HOyibi1
tyMlllHRHdvW4lYJrAj1ouS23ap7PVr8khAtd81H93czd0ZH/MNMlS0bbdgLVmXG
kg6jkT3lp1/hV0Qbs8wNuPdhNJlpdFZs8uQPIdY0xlAk+cJ/V6uRIy77nExyaHKu
SAH8jeli79aNHpSMRMLtRVTfV86qKaudy7k+qtU4pTCZ3etqjExlMQHZQ0AtQdnQ
7zw0rTng8ptyIZ+jPhSXzSdurVzH+KgN5HtuRHAGpjqrp8PZUq1psXDH7YYpaw3E
6DX47+gasa4PcymU+/Ii6mFuq4EVmQx3cZMxV8pjXJNfWYmzJqSTgxlE+BehYR4R
ERBkyXw=
-----END CERTIFICATE-----`)

	selfSignedRootPEM = []byte(`-----BEGIN CERTIFICATE-----
MIIFFDCCAvygAwIBAgIUNqKy6bVpHV/iFFrAF8ixPM7i76YwDQYJKoZIhvcNAQEL
BQAwIjEOMAwGA1UECgwFSXN0aW8xEDAOBgNVBAMMB1Jvb3QgQ0EwHhcNMTkxMDI4
MTkzNjE3WhcNMjkxMDI1MTkzNjE3WjAiMQ4wDAYDVQQKDAVJc3RpbzEQMA4GA1UE
AwwHUm9vdCBDQTCCAiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBAN2V7SiC
800wjfWPSki+n57xOv3o1/+WYhu5k2zeZcZrEPoqeESiKfSWWZ/xvZRVOl2lO137
nyWr1N5GUYMFVXOLJRcK6LTQYZOUWIkSU4JisftxJG+fbz0UJr6YUTWsAcZu4dIh
FtOfBAniit+3qmrBtk1eiJ+abvMM/ricqZGC9WpZXrsJT8cc7BAgQ+OYGZhWwmTD
gClRrJiloUOZknUnvNSvH7QTWR90o1sJ4yhcCmgcuc1yplGJv41Ob7/lks2/jphQ
KwNIqNLJyYRt4AnscKXuSqqGG+LvyrvyvGVyEG+FOl/dd8KigASYSTcSfv5siA20
gs803aEEr5YmMCL4yYw6j6pl/9zZ/iU5gfwRayKug6TyruqT3FL1gIifqgltUJVd
ep9qMW0ms6q+7cYekiZKY3/w9wCBsaiYcDXg87MlsjhfOxHxJNkXO0U0sZpuLWAw
lGaq5vZWk8qTUh9ouzXRklLGtfoUMcbvDDLqGlwRVuntlWKshHxLsLv4DmrOm48L
7aa4DYp/Oo6HOwkXSRbjK01PeWELauwsgo0jv3swFJb0T0lnNq6PvH8zK/yj844J
F2N4WBAOt9ya0frDRWXVNFUL8MpF8su/ohJa/+FUc/tHhemb8c3YoWxTz2GpTB3y
/Ndx0U7wXRMcl5Sh/zNFhG1w8OaEsQKT+BdDAgMBAAGjQjBAMB0GA1UdDgQWBBSk
+BekFIGIitdUVxhd4aXyUFKtwDAPBgNVHRMBAf8EBTADAQH/MA4GA1UdDwEB/wQE
AwIC5DANBgkqhkiG9w0BAQsFAAOCAgEAyTCP0MsFDW0ZKDV3uaIvtjRHtxpiJDuc
F/HsLmSkvDZiO8iB04x0cj07cXachWneodocDxttKIz2zFGeCZY0G71ahkI+2+4N
U05Zc4YB38edS1XZrEP0VFCBWPQKTt4i+Hji/VqfP7QHZkbvGD3fvct6/WOfLg0j
JXeAZSw3W7SMHoYHf8/3adVu/MBZ/RJgkYLIfNYnWMZ0KJ4RjmHx/vR5KlGok5DB
lO3980ZozE7SUCvBehXFoO0ZHSckjCvCYOlDVcTsmKn6R10rxSrIPy7m4zFRNMG2
s340FD/K/v+CAs6JJeyIWMI+Jj69T6wdbPzahmyyAdzrgmKojFnmZWpgLDwU75GE
UxISkvWyfK4T08voNdbuEA0qJyLOYgDWq8lzSX+CrOyyzSZmq66u7x8P9h09kek+
Oq0m3+PYymUOFoUMQpYq8Eti69l52sEDszNGUrYP7pnoI+2c134aLRiFH7PRmXm1
xLiri6SOsFK0jsZwsT+Envb84KU5CVZXcdYTUzK5nuKdFUd2b+tUOC3+DfKFdHhb
GHQv5tKYlfhgb0qcNc2NMpEQXBZBIMo45iaYvRyXsgYLjX2tcv0vPBOwi0b7sgb5
EPpSJKObiBraYaKr24TAGoto2Z8aNs2ZaJBsrCOoaGKq8FtP5dGkblXPYp7KLpuv
Ej9AJ9YUvkw=
-----END CERTIFICATE-----`)

	externalCASecret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cacerts",
			Namespace: testNamespace,
		},
		Data: map[string][]byte{
			"ca-cert.pem":   caCertPEM,
			"root-cert.pem": rootCertPEM,
		},
	}

	selfSignedCASecret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-ca-secret",
			Namespace: testNamespace,
		},
		Data: map[string][]byte{
			"ca-cert.pem": selfSignedRootPEM,
		},
	}

	badSelfSignedSecret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-ca-secret",
			Namespace: testNamespace,
		},
		Data: map[string][]byte{"ca-cert.pem": []byte("bad cert")},
	}

	rootCertX509, caCertX509, selfSignedX509 *x509.Certificate
)

func init() {
	parsePEM := func(in []byte) *x509.Certificate {
		p, _ := pem.Decode(in)
		cert, err := x509.ParseCertificate(p.Bytes)
		if err != nil {
			panic(fmt.Sprintf("failed to create test cert: %v", err))
		}
		return cert
	}
	rootCertX509 = parsePEM(rootCertPEM)
	caCertX509 = parsePEM(caCertPEM)
	selfSignedX509 = parsePEM(selfSignedRootPEM)
}

func TestReadCACerts(t *testing.T) {
	applyTestCases := []struct {
		name         string
		objs         []runtime.Object
		listFailure  error
		getFailure   error
		parseFailure error
		want         *CACerts
	}{
		//{
		//	name:        "list failure",
		//	objs:        []runtime.Object{externalCASecret, selfSignedCASecret, kubeSystemNamespace},
		//	listFailure: errors.New("list failure"),
		//	want:        &CACerts{},
		//},
		//{
		//	name:       "get failure",
		//	objs:       []runtime.Object{externalCASecret, selfSignedCASecret, kubeSystemNamespace},
		//	getFailure: errors.New("get failure"),
		//	want:       &CACerts{},
		//},
		{
			name: "cert parse failure",
			objs: []runtime.Object{externalCASecret, badSelfSignedSecret, kubeSystemNamespace},
			want: &CACerts{
				externalCACert:   caCertX509,
				externalRootCert: rootCertX509,
			},
		},
		{
			name: "success",
			objs: []runtime.Object{externalCASecret, selfSignedCASecret, kubeSystemNamespace},
			want: &CACerts{
				externalCACert:   caCertX509,
				externalRootCert: rootCertX509,
				selfSignedCACert: selfSignedX509,
			},
		},
	}

	for i := range applyTestCases {
		c := &applyTestCases[i]
		t.Run(fmt.Sprintf("[%v] %v", i, c.name), func(tt *testing.T) {
			g := NewWithT(tt)

			env, cluster := createTestClusterAndEnvOrDie(t, "context0", nil, goodClusterDesc, c.objs...)

			if c.listFailure != nil {
				reaction := func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, c.listFailure
				}
				env.client.PrependReactor("list", "secrets", reaction)
			}

			if c.getFailure != nil {
				reaction := func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, c.getFailure
				}
				env.client.PrependReactor("get", "secrets", reaction)
			}

			got := cluster.readCACerts(env)
			g.Expect(got.externalRootCert).To(Equal(c.want.externalRootCert), "Root certs should match")
			g.Expect(got.externalCACert).To(Equal(c.want.externalCACert), "CA certs should match")
			g.Expect(got.selfSignedCACert).To(Equal(c.want.selfSignedCACert), "SelfSigned certs should match")
		})
	}
}

type address struct {
	ip       string
	hostname string
}

func serviceStatus(addresses ...address) *v1.ServiceStatus {
	status := &v1.ServiceStatus{
		LoadBalancer: v1.LoadBalancerStatus{},
	}

	for _, address := range addresses {
		if address.ip == "" && address.hostname == "" {
			continue
		}
		status.LoadBalancer.Ingress = append(status.LoadBalancer.Ingress,
			v1.LoadBalancerIngress{
				IP:       address.ip,
				Hostname: address.hostname,
			},
		)
	}

	return status
}

func TestReadIngressGatewayAddresses(t *testing.T) {
	g := NewGomegaWithT(t)

	applyTestCases := []struct {
		in      *v1.ServiceStatus
		cluster *Cluster
		want    []*Gateway
	}{
		{
			in:   serviceStatus(),
			want: []*Gateway{},
		},
		{
			in: serviceStatus(address{ip: "192.168.1.0"}),
			want: []*Gateway{
				{Address: "192.168.1.0", Port: 443},
			},
		},
		{
			in: serviceStatus(address{ip: "192.168.1.0"}, address{ip: "192.168.1.1"}),
			want: []*Gateway{
				{Address: "192.168.1.0", Port: 443},
				{Address: "192.168.1.1", Port: 443},
			},
		},
		{
			in: serviceStatus(address{hostname: "foo.example.com"}),
			want: []*Gateway{
				{Address: "foo.example.com", Port: 443},
			},
		},
		{
			in: serviceStatus(address{hostname: "foo.example.com"}, address{ip: "192.168.1.1"}),
			want: []*Gateway{
				{Address: "foo.example.com", Port: 443},
				{Address: "192.168.1.1", Port: 443},
			},
		},
	}

	for _, c := range applyTestCases {
		g.Expect(gatewaysFromServiceStatus(c.in, c.cluster)).To(Equal(c.want))
	}
}
