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
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/gomega"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd/api"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/assert"
)

var (
	kubeSystemNamespaceUID = types.UID("54643f96-eca0-11e9-bb97-42010a80000a")
	kubeSystemNamespace    = &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kube-system",
			UID:  kubeSystemNamespaceUID,
		},
	}
)

const (
	testNamespace          = "istio-system-test"
	testServiceAccountName = "test-service-account"
)

func makeServiceAccount(secrets ...string) *v1.ServiceAccount {
	sa := &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testServiceAccountName,
			Namespace: testNamespace,
		},
	}

	for _, secret := range secrets {
		sa.Secrets = append(sa.Secrets, v1.ObjectReference{
			Name:      secret,
			Namespace: testNamespace,
		})
	}

	return sa
}

func makeSecret(name, caData, token string) *v1.Secret {
	out := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   testNamespace,
			Annotations: map[string]string{v1.ServiceAccountNameKey: testServiceAccountName},
		},
		Data: map[string][]byte{},
		Type: v1.SecretTypeServiceAccountToken,
	}
	if len(caData) > 0 {
		out.Data[v1.ServiceAccountRootCAKey] = []byte(caData)
	}
	if len(token) > 0 {
		out.Data[v1.ServiceAccountTokenKey] = []byte(token)
	}
	return out
}

type fakeOutputWriter struct {
	b           bytes.Buffer
	injectError error
	failAfter   int
}

func (w *fakeOutputWriter) Write(p []byte) (n int, err error) {
	w.failAfter--
	if w.failAfter <= 0 && w.injectError != nil {
		return 0, w.injectError
	}
	return w.b.Write(p)
}
func (w *fakeOutputWriter) String() string { return w.b.String() }

func TestCreateRemoteSecrets(t *testing.T) {
	prevOutputWriterStub := makeOutputWriterTestHook
	defer func() { makeOutputWriterTestHook = prevOutputWriterStub }()

	prevTokenWaitBackoff := tokenWaitBackoff
	defer func() { tokenWaitBackoff = prevTokenWaitBackoff }()

	sa := makeServiceAccount("saSecret")
	sa2 := makeServiceAccount("saSecret", "saSecret2")
	saSecret := makeSecret("saSecret", "caData", "token")
	saSecret2 := makeSecret("saSecret2", "caData", "token")
	saSecretMissingToken := makeSecret("saSecret", "caData", "")

	tokenWaitBackoff = 10 * time.Millisecond

	cases := []struct {
		testName string

		objs       []runtime.Object
		name       string
		secType    SecretType
		secretName string

		// inject errors
		badStartingConfig bool
		outputWriterError error

		want            string
		wantErrStr      string
		k8sMinorVersion string
	}{
		{
			testName:   "fail to create remote secret token",
			objs:       []runtime.Object{kubeSystemNamespace, sa, saSecretMissingToken},
			wantErrStr: `no "token" data found`,
		},
		{
			testName:          "fail to encode secret",
			objs:              []runtime.Object{kubeSystemNamespace, sa, saSecret},
			outputWriterError: errors.New("injected encode error"),
			wantErrStr:        "injected encode error",
		},
		{
			testName: "success",
			objs:     []runtime.Object{kubeSystemNamespace, sa, saSecret},
			name:     "cluster-foo",
			want:     "cal-want",
		},
		{
			testName: "success with type defined",
			objs:     []runtime.Object{kubeSystemNamespace, sa, saSecret},
			name:     "cluster-foo",
			secType:  "config",
			want:     "cal-want",
		},
		{
			testName:   "failure due to multiple secrets",
			objs:       []runtime.Object{kubeSystemNamespace, sa2, saSecret, saSecret2},
			name:       "cluster-foo",
			wantErrStr: "wrong number of secrets (2) in serviceaccount",
			// for k8s 1.24+ we auto-create a secret instead of relying on a reference in service account
			k8sMinorVersion: "23",
		},
		{
			testName:   "success when specific secret name provided",
			objs:       []runtime.Object{kubeSystemNamespace, sa2, saSecret, saSecret2},
			secretName: saSecret.Name,
			name:       "cluster-foo",
			want:       "cal-want",
		},
		{
			testName:        "fail when non-existing secret name provided",
			objs:            []runtime.Object{kubeSystemNamespace, sa2, saSecret, saSecret2},
			secretName:      "nonexistingSecret",
			name:            "cluster-foo",
			want:            "cal-want",
			wantErrStr:      "provided secret does not exist",
			k8sMinorVersion: "23",
		},
	}

	for i := range cases {
		c := &cases[i]
		t.Run(fmt.Sprintf("[%v] %v", i, c.testName), func(tt *testing.T) {
			makeOutputWriterTestHook = func() writer {
				return &fakeOutputWriter{injectError: c.outputWriterError}
			}
			if c.secType != SecretTypeConfig {
				c.secType = SecretTypeRemote
			}
			opts := RemoteSecretOptions{
				ServiceAccountName: testServiceAccountName,
				AuthType:           RemoteSecretAuthTypeBearerToken,
				// ClusterName: testCluster,
				KubeOptions: KubeOptions{
					Namespace: testNamespace,
				},
				Type:       c.secType,
				SecretName: c.secretName,
			}

			ctx := cli.NewFakeContext(&cli.NewFakeContextOption{
				IstioNamespace: "istio-system",
				Objects:        c.objs,
				Namespace:      testNamespace,
				Version:        c.k8sMinorVersion,
			})
			client, err := ctx.CLIClient()
			if err != nil {
				tt.Fatalf("failed to create client: %v", err)
			}
			got, _, err := CreateRemoteSecret(client, opts)
			if c.wantErrStr != "" {
				if err == nil {
					tt.Fatalf("wanted error including %q but got none", c.wantErrStr)
				} else if !strings.Contains(err.Error(), c.wantErrStr) {
					tt.Fatalf("wanted error including %q but got %v", c.wantErrStr, err)
				}
			} else if c.wantErrStr == "" && err != nil {
				tt.Fatalf("wanted non-error but got %q", err)
			} else if c.want != "" {
				var secretName, key string
				switch c.secType {
				case SecretTypeConfig:
					secretName = configSecretName
					key = configSecretKey
				default:
					secretName = remoteSecretPrefix + string(kubeSystemNamespaceUID)
					key = "54643f96-eca0-11e9-bb97-42010a80000a"
				}
				wantOutput := fmt.Sprintf(`# This file is autogenerated, do not edit.
apiVersion: v1
kind: Secret
metadata:
  annotations:
    %s: 54643f96-eca0-11e9-bb97-42010a80000a
  creationTimestamp: null
  labels:
    istio/multiCluster: "true"
  name: %s
  namespace: istio-system-test
stringData:
  %s: |
    apiVersion: v1
    clusters:
    - cluster:
        certificate-authority-data: Y2FEYXRh
        server: server
      name: 54643f96-eca0-11e9-bb97-42010a80000a
    contexts:
    - context:
        cluster: 54643f96-eca0-11e9-bb97-42010a80000a
        user: 54643f96-eca0-11e9-bb97-42010a80000a
      name: 54643f96-eca0-11e9-bb97-42010a80000a
    current-context: 54643f96-eca0-11e9-bb97-42010a80000a
    kind: Config
    preferences: {}
    users:
    - name: 54643f96-eca0-11e9-bb97-42010a80000a
      user:
        token: token
---
`, clusterNameAnnotationKey, secretName, key)

				if diff := cmp.Diff(got, wantOutput); diff != "" {
					tt.Errorf("got\n%v\nwant\n%vdiff %v", got, c.want, diff)
				}
			}
		})
	}
}

func TestGetServiceAccountSecretToken(t *testing.T) {
	secret := makeSecret("secret", "caData", "token")

	type tc struct {
		name string
		opts RemoteSecretOptions
		objs []runtime.Object

		want       *v1.Secret
		wantErrStr string
	}

	commonCases := []tc{
		{
			name: "missing service account",
			opts: RemoteSecretOptions{
				ServiceAccountName: testServiceAccountName,
				KubeOptions: KubeOptions{
					Namespace: testNamespace,
				},
				ManifestsPath: filepath.Join(env.IstioSrc, "manifests"),
			},
			wantErrStr: fmt.Sprintf("serviceaccounts %q not found", testServiceAccountName),
		},
	}

	legacyCases := append([]tc{
		{
			name: "wrong number of secrets",
			opts: RemoteSecretOptions{
				ServiceAccountName:   testServiceAccountName,
				CreateServiceAccount: false,
				KubeOptions: KubeOptions{
					Namespace: testNamespace,
				},
				ManifestsPath: filepath.Join(env.IstioSrc, "manifests"),
			},
			objs: []runtime.Object{
				makeServiceAccount("secret", "extra-secret"),
			},
			wantErrStr: "wrong number of secrets",
		},
		{
			name: "missing service account token secret",
			opts: RemoteSecretOptions{
				ServiceAccountName: testServiceAccountName,
				KubeOptions: KubeOptions{
					Namespace: testNamespace,
				},
				ManifestsPath: filepath.Join(env.IstioSrc, "manifests"),
			},
			objs: []runtime.Object{
				makeServiceAccount("wrong-secret"),
				secret,
			},
			wantErrStr: `secrets "wrong-secret" not found`,
		},
		{
			name: "success",
			opts: RemoteSecretOptions{
				ServiceAccountName: testServiceAccountName,
				KubeOptions: KubeOptions{
					Namespace: testNamespace,
				},
				ManifestsPath: filepath.Join(env.IstioSrc, "manifests"),
			},
			objs: []runtime.Object{
				makeServiceAccount("secret"),
				secret,
			},
			want: secret,
		},
	}, commonCases...)

	cases := append([]tc{
		{
			name: "success",
			opts: RemoteSecretOptions{
				ServiceAccountName: testServiceAccountName,
				KubeOptions: KubeOptions{
					Namespace: testNamespace,
				},
				ManifestsPath: filepath.Join(env.IstioSrc, "manifests"),
			},
			objs: []runtime.Object{
				makeServiceAccount(tokenSecretName(testServiceAccountName)),
			},
			want: &v1.Secret{
				TypeMeta: metav1.TypeMeta{Kind: gvk.Secret.Kind, APIVersion: gvr.Secret.GroupVersion().String()},
				ObjectMeta: metav1.ObjectMeta{
					Name:        tokenSecretName(testServiceAccountName),
					Namespace:   testNamespace,
					Annotations: map[string]string{v1.ServiceAccountNameKey: testServiceAccountName},
				},
				Type: v1.SecretTypeServiceAccountToken,
			},
		},
	}, commonCases...)

	doCase := func(t *testing.T, c tc, k8sMinorVer string) {
		t.Run(fmt.Sprintf("%v", c.name), func(tt *testing.T) {
			client := kube.NewFakeClientWithVersion(k8sMinorVer, c.objs...)
			got, err := getServiceAccountSecret(client, c.opts)
			if err == nil {
				got.ManagedFields = nil
			}
			if c.wantErrStr != "" {
				if err == nil {
					tt.Fatalf("wanted error including %q but got none", c.wantErrStr)
				} else if !strings.Contains(err.Error(), c.wantErrStr) {
					tt.Fatalf("wanted error including %q but got %v", c.wantErrStr, err)
				}
			} else if c.wantErrStr == "" && err != nil {
				tt.Fatalf("wanted non-error but got %q", err)
			} else {
				assert.Equal(t, got, c.want)
			}
		})
	}

	t.Run("kubernetes created secret (legacy)", func(t *testing.T) {
		for _, c := range legacyCases {
			doCase(t, c, "23")
		}
	})
	t.Run("istioctl created secret", func(t *testing.T) {
		for _, c := range cases {
			doCase(t, c, "")
		}
	})
}

func TestGenerateServiceAccount(t *testing.T) {
	opts := RemoteSecretOptions{
		CreateServiceAccount: true,
		ManifestsPath:        filepath.Join(env.IstioSrc, "manifests"),
		KubeOptions: KubeOptions{
			Namespace: "istio-system",
		},
	}
	yaml, err := generateServiceAccountYAML(opts)
	if err != nil {
		t.Fatalf("failed to generate service account YAML: %v", err)
	}
	objs, err := manifest.ParseMultiple(yaml)
	if err != nil {
		t.Fatal(err)
	}

	mustFindObject(t, objs, "istio-reader-service-account", "ServiceAccount")
	mustFindObject(t, objs, "istio-reader-clusterrole-istio-system", "ClusterRole")
	mustFindObject(t, objs, "istio-reader-clusterrole-istio-system", "ClusterRoleBinding")
}

func mustFindObject(t test.Failer, objs []manifest.Manifest, name, kind string) {
	t.Helper()
	var obj *manifest.Manifest
	for _, o := range objs {
		if o.GetKind() == kind && o.GetName() == name {
			obj = &o
			break
		}
	}
	if obj == nil {
		t.Fatalf("expected %v/%v", name, kind)
	}
}

func TestCreateRemoteKubeconfig(t *testing.T) {
	fakeClusterName := "fake-clusterName-0"
	kubeconfig := strings.ReplaceAll(`apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: Y2FEYXRh
    server: https://1.2.3.4
  name: {cluster}
contexts:
- context:
    cluster: {cluster}
    user: {cluster}
  name: {cluster}
current-context: {cluster}
kind: Config
preferences: {}
users:
- name: {cluster}
  user:
    token: token
`, "{cluster}", fakeClusterName)

	cases := []struct {
		name               string
		clusterName        string
		context            string
		server             string
		haveTokenSecret    *v1.Secret
		updatedTokenSecret *v1.Secret
		wantRemoteSecret   *v1.Secret
		wantErrStr         string
	}{
		{
			name:            "missing caData",
			haveTokenSecret: makeSecret("", "", "token"),
			context:         "c0",
			clusterName:     fakeClusterName,
			wantErrStr:      errMissingRootCAKey.Error(),
		},
		{
			name:            "missing token",
			haveTokenSecret: makeSecret("", "caData", ""),
			context:         "c0",
			clusterName:     fakeClusterName,
			wantErrStr:      errMissingTokenKey.Error(),
		},
		{
			name:            "bad server name",
			haveTokenSecret: makeSecret("", "caData", "token"),
			context:         "c0",
			clusterName:     fakeClusterName,
			server:          "",
			wantErrStr:      "invalid kubeconfig:",
		},
		{
			name:               "success after wait",
			haveTokenSecret:    makeSecret("", "caData", ""),
			updatedTokenSecret: makeSecret("", "caData", "token"), // token is populated later
			context:            "c0",
			clusterName:        fakeClusterName,
			server:             "https://1.2.3.4",
			wantRemoteSecret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: remoteSecretNameFromClusterName(fakeClusterName),
					Annotations: map[string]string{
						clusterNameAnnotationKey: fakeClusterName,
					},
					Labels: map[string]string{
						multicluster.MultiClusterSecretLabel: "true",
					},
				},
				Data: map[string][]byte{
					fakeClusterName: []byte(kubeconfig),
				},
			},
		},
		{
			name:            "success",
			haveTokenSecret: makeSecret("", "caData", "token"),
			context:         "c0",
			clusterName:     fakeClusterName,
			server:          "https://1.2.3.4",
			wantRemoteSecret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: remoteSecretNameFromClusterName(fakeClusterName),
					Annotations: map[string]string{
						clusterNameAnnotationKey: fakeClusterName,
					},
					Labels: map[string]string{
						multicluster.MultiClusterSecretLabel: "true",
					},
				},
				Data: map[string][]byte{
					fakeClusterName: []byte(kubeconfig),
				},
			},
		},
	}
	oldBackoff := tokenWaitBackoff
	tokenWaitBackoff = time.Millisecond
	t.Cleanup(func() {
		tokenWaitBackoff = oldBackoff
	})
	for i := range cases {
		c := &cases[i]
		secName := remoteSecretNameFromClusterName(c.clusterName)
		t.Run(fmt.Sprintf("[%v] %v", i, c.name), func(tt *testing.T) {
			// no updateTokenSecret means re-fetching yields the same result
			obj := []runtime.Object{c.haveTokenSecret}
			if c.updatedTokenSecret != nil {
				// fetching should give a different result than the token secret we pass in
				obj = []runtime.Object{c.updatedTokenSecret}
			}
			client := kube.NewFakeClient(obj...)

			got, err := createRemoteSecretFromTokenAndServer(client, c.haveTokenSecret, c.clusterName, c.server, secName)
			if c.wantErrStr != "" {
				if err == nil {
					tt.Fatalf("wanted error including %q but none", c.wantErrStr)
				} else if !strings.Contains(err.Error(), c.wantErrStr) {
					tt.Fatalf("wanted error including %q but %v", c.wantErrStr, err)
				}
			} else if c.wantErrStr == "" && err != nil {
				tt.Fatalf("wanted non-error but got %q", err)
			} else if diff := cmp.Diff(got, c.wantRemoteSecret); diff != "" {
				tt.Fatalf(" got %v\nwant %v\ndiff %v", got, c.wantRemoteSecret, diff)
			}
		})
	}
}

func TestWriteEncodedSecret(t *testing.T) {
	s := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},
	}

	w := &fakeOutputWriter{failAfter: 0, injectError: errors.New("error")}
	if err := writeEncodedObject(w, s); err == nil {
		t.Error("want error on local write failure")
	}

	w = &fakeOutputWriter{failAfter: 1, injectError: errors.New("error")}
	if err := writeEncodedObject(w, s); err == nil {
		t.Error("want error on remote write failure")
	}

	w = &fakeOutputWriter{failAfter: 2, injectError: errors.New("error")}
	if err := writeEncodedObject(w, s); err == nil {
		t.Error("want error on third write failure")
	}

	w = &fakeOutputWriter{}
	if err := writeEncodedObject(w, s); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	want := `# This file is autogenerated, do not edit.
apiVersion: v1
kind: Secret
metadata:
  creationTimestamp: null
  name: foo
---
`
	if w.String() != want {
		t.Errorf("got\n%q\nwant\n%q", w.String(), want)
	}
}

func TestCreateRemoteSecretFromPlugin(t *testing.T) {
	fakeClusterName := "fake-clusterName-0"
	kubeconfig := strings.ReplaceAll(`apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: Y2FEYXRh
    server: https://1.2.3.4
  name: {cluster}
contexts:
- context:
    cluster: {cluster}
    user: {cluster}
  name: {cluster}
current-context: {cluster}
kind: Config
preferences: {}
users:
- name: {cluster}
  user:
    auth-provider:
      config:
        k1: v1
      name: foobar
`, "{cluster}", fakeClusterName)

	cases := []struct {
		name               string
		in                 *v1.Secret
		context            string
		clusterName        string
		server             string
		authProviderConfig *api.AuthProviderConfig
		want               *v1.Secret
		wantErrStr         string
	}{
		{
			name:        "error on missing caData",
			in:          makeSecret("", "", "token"),
			context:     "c0",
			clusterName: fakeClusterName,
			wantErrStr:  errMissingRootCAKey.Error(),
		},
		{
			name:        "success on missing token",
			in:          makeSecret("", "caData", ""),
			context:     "c0",
			clusterName: fakeClusterName,
			server:      "https://1.2.3.4",
			authProviderConfig: &api.AuthProviderConfig{
				Name: "foobar",
				Config: map[string]string{
					"k1": "v1",
				},
			},
			want: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: remoteSecretNameFromClusterName(fakeClusterName),
					Annotations: map[string]string{
						clusterNameAnnotationKey: fakeClusterName,
					},
					Labels: map[string]string{
						multicluster.MultiClusterSecretLabel: "true",
					},
				},
				Data: map[string][]byte{
					fakeClusterName: []byte(kubeconfig),
				},
			},
		},
		{
			name:        "success",
			in:          makeSecret("", "caData", "token"),
			context:     "c0",
			clusterName: fakeClusterName,
			server:      "https://1.2.3.4",
			authProviderConfig: &api.AuthProviderConfig{
				Name: "foobar",
				Config: map[string]string{
					"k1": "v1",
				},
			},
			want: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: remoteSecretNameFromClusterName(fakeClusterName),
					Annotations: map[string]string{
						clusterNameAnnotationKey: fakeClusterName,
					},
					Labels: map[string]string{
						multicluster.MultiClusterSecretLabel: "true",
					},
				},
				Data: map[string][]byte{
					fakeClusterName: []byte(kubeconfig),
				},
			},
		},
	}

	for i := range cases {
		c := &cases[i]
		secName := remoteSecretNameFromClusterName(c.clusterName)
		t.Run(fmt.Sprintf("[%v] %v", i, c.name), func(tt *testing.T) {
			got, err := createRemoteSecretFromPlugin(c.in, c.server, c.clusterName, secName, c.authProviderConfig)
			if c.wantErrStr != "" {
				if err == nil {
					tt.Fatalf("wanted error including %q but none", c.wantErrStr)
				} else if !strings.Contains(err.Error(), c.wantErrStr) {
					tt.Fatalf("wanted error including %q but %v", c.wantErrStr, err)
				}
			} else if c.wantErrStr == "" && err != nil {
				tt.Fatalf("wanted non-error but got %q", err)
			} else if diff := cmp.Diff(got, c.want); diff != "" {
				tt.Fatalf(" got %v\nwant %v\ndiff %v", got, c.want, diff)
			}
		})
	}
}

func TestRemoteSecretOptions(t *testing.T) {
	g := NewWithT(t)

	ctx := cli.NewFakeContext(nil)
	o := RemoteSecretOptions{}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	o.addFlags(flags)
	g.Expect(flags.Parse([]string{
		"--name",
		"valid-name",
	})).Should(Succeed())
	g.Expect(o.prepare(ctx)).Should(Succeed())

	o = RemoteSecretOptions{}
	flags = pflag.NewFlagSet("test", pflag.ContinueOnError)
	o.addFlags(flags)
	g.Expect(flags.Parse([]string{
		"--name",
		"?-invalid-name",
	})).Should(Succeed())
	g.Expect(o.prepare(ctx)).Should(Not(Succeed()))
}
