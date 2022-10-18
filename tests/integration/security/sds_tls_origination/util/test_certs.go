//go:build integ
// +build integ

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

package sdstlsutil

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/go-multierror"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/util/retry"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	FakeRoot = `-----BEGIN CERTIFICATE-----
MIIDEzCCAfugAwIBAgIUFN9C752sxgbtG2/66BsCrNKstmswDQYJKoZIhvcNAQEL
BQAwGDEWMBQGA1UEAwwNY2x1c3Rlci5sb2NhbDAgFw0yMDA3MTUxODQ3MzBaGA8y
Mjk0MDQzMDE4NDczMFowGDEWMBQGA1UEAwwNY2x1c3Rlci5sb2NhbDCCASIwDQYJ
KoZIhvcNAQEBBQADggEPADCCAQoCggEBAPAfVJJt6oS5x2NfdBX5ytaogjgaJGlE
t8f9mZIeDDMJbiGk4/jHzjSwy8TS77vfvSE9cGGSPNaiAJE1hTQ8AsMeU6+zZeKc
Hg456L+yFxYoDNmS2G/Xf9+po/RyB56vBKxqttU7cU6hOdPJfI4r+y7wtabH7Dyk
iP4XLTEMtyDuc6WSLps3i4PfaR3yZ1rRTqB/tkFWLTll5S5Hyv/bUllfycA2sHAI
ydSoITwy22598wGYhu+mAt+pa56sL/Icwo6bOGrd30zvYkIKLtTUtDP7WeLwz+04
IVqHrCwfgDqWTuuQgJ1P+PR/p18rvP9zZywE6RnS46IiInCkNYCZvrkCAwEAAaNT
MFEwHQYDVR0OBBYEFOr2EDH9SkP40ekPYmFymbJXfjY/MB8GA1UdIwQYMBaAFOr2
EDH9SkP40ekPYmFymbJXfjY/MA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQEL
BQADggEBAKmj6brm2Judz/kdj22drbnApnbDhi59w4yaGaByNTiEyxs+jvDuWNmj
xnIU27xeZTmDdKw+JrD++g4p5ijOoBMA5wHRzAehQyjrolJ+C+cJG1L2kF8XTrfh
AYXySgMMfkHlsz2AUQISRt5ghyXuW5d9EizwQxFFJ/USDNNjGyc8IFyykklJUEEY
bZbejB2iiQJFISPoaqaRcbM3B8fSOfiQXnTjcAshB/aA2Gx7JCN+4dXyOZKzc/1c
GTfQSrehPD0K5bjPgXibCNhUYSoRsQaKLmztkW05+1FgndckeE2s3XjKolzwRUod
yQsU2FDC7Nou8HganGBNHdlxA0FW14A=
-----END CERTIFICATE-----`
	FakeCert = `-----BEGIN CERTIFICATE-----
MIIDGDCCAgCgAwIBAgIUfRMDVetBzpYckNs7A03PV/POE8swDQYJKoZIhvcNAQEL
BQAwGDEWMBQGA1UEAwwNY2x1c3Rlci5sb2NhbDAgFw0yMDA3MTUxODQ4NDVaGA8y
Mjk0MDQzMDE4NDg0NVowGDEWMBQGA1UEAwwNY2x1c3Rlci5sb2NhbDCCASIwDQYJ
KoZIhvcNAQEBBQADggEPADCCAQoCggEBAMUzjpCwwX6ZoslSIq/aiq8DW0V0vBk9
OKibZIluu1ejoGz2r7OB8WM9DpQz/ma6V1tAyqoLY+ll7rKUw+B6Yd0Lw0v5VMBU
mBqJTYzrYDXgVPhqRvRPNWMgb5JExnXagsizGrXaXeiB/MzOSN5fxQsYmy52/aMc
IeIZYxRM5IGUDlv4rSm5Y2V0s4jnJY+izJSiRfSwXtGbrbZ1MvL3iYCERMDOLnA+
O3esEi2c5X231jyqau6ASFOkk9YYfQ6n49m9s5MXCc/zBHMvOdYVfF4vQeNy+qbs
3/3LQab/aoRgX3lcvvcHmXtwZJATqEbxCZnNu10fU4D2s2brLxTNLo0CAwEAAaNY
MFYwCQYDVR0TBAIwADALBgNVHQ8EBAMCBeAwHQYDVR0lBBYwFAYIKwYBBQUHAwIG
CCsGAQUFBwMBMB0GA1UdEQQWMBSCEnNlcnZlci5kZWZhdWx0LnN2YzANBgkqhkiG
9w0BAQsFAAOCAQEAiVW8NcH5ZBIFTccXgtBXEhxRGLxh674y/pnZ2V44YHaLTL8K
m/r2pgzPKAMVW9wpizaJqvJwqPVCaEAE2XtqKX4rwI1OVGexO+66Acor46C6ocM9
3PBrhPX7YkK31hKRHqMdTyMePRDoK4cIRjUCfrom7eWO9BS1xQXVovojvJi1Y7Zj
z45eIpSu9Wf1a/e/mAtAOw/Hw91jFeGImEiaRCha6CBxlFtv/BHNbJjwIvEj+N68
AskpylAnBm6O8poi6W+tyHqOE7v4WBp5GyuSeIn9Hc3BK6qU4cW02wBiqlK6Yesh
xaHcTmpFhBNFbtway+TvM/IaIqJ8Eg/MantNLA==
-----END CERTIFICATE-----`
	FakeKey = `-----BEGIN RSA PRIVATE KEY-----
MIIEpgIBAAKCAQEAxTOOkLDBfpmiyVIir9qKrwNbRXS8GT04qJtkiW67V6OgbPav
s4HxYz0OlDP+ZrpXW0DKqgtj6WXuspTD4Hph3QvDS/lUwFSYGolNjOtgNeBU+GpG
9E81YyBvkkTGddqCyLMatdpd6IH8zM5I3l/FCxibLnb9oxwh4hljFEzkgZQOW/it
KbljZXSziOclj6LMlKJF9LBe0ZuttnUy8veJgIREwM4ucD47d6wSLZzlfbfWPKpq
7oBIU6ST1hh9Dqfj2b2zkxcJz/MEcy851hV8Xi9B43L6puzf/ctBpv9qhGBfeVy+
9weZe3BkkBOoRvEJmc27XR9TgPazZusvFM0ujQIDAQABAoIBAQCDkN4w0nyFxmLB
Bjd2M8wK76ZZNIS6IgpHE0WEG4iJ8/T4Pa0DilJN71Jmtjmot/HIQ/XydR73fLZA
FtiIT54zJ8HoUjSlDMteCPTga7kIuN53zhAAt0fbFqzZXWE7B8nxtOzBHytAEFll
Gsuq8SI5QPVnjqOxyvcgLefYh2R8venbrPeM+csICengeiXd2JaNGurajm3vRzBG
BSHTQ5APf8y7u0UsYT+xmY6Yt2dll1mNPfwByTVwDWzZjlHTWGASjwr24yzeSijl
5QYeEsgciPC7Bs39X9ZwERgVEENqJqEyokRiXbapdU9xpqdqw4QWTkzn2zQMJ2CD
awz+0WkBAoGBAPTa+9M3NgIlJjfS+WtMhK4laTymOOBDFpcaAC3dd0u13EeVE/h8
6f3d4PXqlVfzvpDiwpTJTuBQ3PmJ6qYgs1yhafZJSP8hUd+efLJyoOO5BTSxYR/S
TI4gKUYtTky/YReFjOSU0MDXQir6PKPPfY8CJlvaDjkjkmk1BNQaUHzRAoGBAM4t
UPSuYKwE9KhhnihJoynac+PfkTrLMY2ZZ4H0hJn0EBx8UHc/Gc7nJCCUxpYO0TR2
UPu8MhcKs8LRyv0p/YBXRQXgu1qyzclUfK/i5lsmM0V/H4Cj+RTPs2fZSe003F2/
qNd51RYxZq80bOWXWtkgdRKE3pZgIp4FVbabyZT9AoGBAOrceZxZYvafx47YUOG4
3bNksxK3peqGr050ZCOaQGlgoVAQEL3So2ccwkFfp6xbYjj7KQUqKvxC1BKPVYHP
7/sz4L2aAeimfy/th1JrXSPRPssSMUUipMfW1YA4yNgY4fp74W8Hx0yRrSgoKq49
wgPAXibQe8AW/MLpVh5Ut0thAoGBAI58qvIugQjg8+Racl8NZQHLw0O8gjXLr5dY
aTxarDlpfqjxEPsYVNG01DbgGs4ht1s2WYlf6o4aC1mce1iy6EsGBOGnClQINkfp
Z7J2cRSVNeHVlQPmToGfeTFP7dNNMO5pQlqIDEemJHz5EjkpfNOJpt8BjIMINWRX
84Cb8ZhRAoGBAM9mwEoCGognroNHz2aHlyd2zsVmWEbPKRSzLpjte7bs7cTjWTFt
AcCmGiv5+fUBaa/AgMDBD3YKxFRkEEx8ns2C/R2EGtSoNbLmoVxPqsc13/rVcR2b
pNCxV9Zp0EA6qmvtb5A+tBo4BTRIZbAZ1jOZcjP4lOl+cA7FoNk6f9OE
-----END RSA PRIVATE KEY-----`
)

// CreateRoleForSecretAccessInNamespace creates Roles in given namespace for listing secrets
func CreateRoleForSecretAccessInNamespace(t framework.TestContext, roleName string, namespace string, clusters ...cluster.Cluster,
) {
	t.Helper()

	// Create Kubernetes role for secret access
	wg := multierror.Group{}
	if len(clusters) == 0 {
		clusters = t.Clusters()
	}
	for _, c := range clusters {
		c := c
		wg.Go(func() error {
			role := &rbacv1.Role{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Role",
					APIVersion: "rbac.authorization.k8s.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      roleName,
					Namespace: namespace,
				},
				Rules: []rbacv1.PolicyRule{
					rbacv1.PolicyRule{
						Verbs:     []string{"list"},
						Resources: []string{"secrets"},
						APIGroups: []string{""},
					},
				},
			}
			_, err := c.Kube().RbacV1().Roles(namespace).Create(context.TODO(), role, metav1.CreateOptions{})
			if err != nil {
				if errors.IsAlreadyExists(err) {
					if _, err := c.Kube().RbacV1().Roles(namespace).Update(context.TODO(), role, metav1.UpdateOptions{}); err != nil {
						return fmt.Errorf("failed to update roles (error: %s)", err)
					}
				} else {
					return fmt.Errorf("failed to update roles (error: %s)", err)
				}
			}
			// Check if Kubernetes role is ready
			return retry.UntilSuccess(func() error {
				_, err := c.Kube().RbacV1().Roles(namespace).Get(context.TODO(), "test", metav1.GetOptions{})
				if err != nil {
					return fmt.Errorf("role %v not found: %v", "test", err)
				}
				return nil
			}, retry.Timeout(time.Second*5))
		})
	}
	if err := wg.Wait().ErrorOrNil(); err != nil {
		t.Fatal(err)
	}
}

// CreateRoleForSecretAccessInNamespace creates RoleBindings in given namespace for
// authorizing specific serviceAccounts to list secrets in the namespace.
func CreateRoleBindingForSecretAccessInNamespace(t framework.TestContext, roleName string, namespace string, subject string, clusters ...cluster.Cluster,
) {
	t.Helper()

	// Create Kubernetes role for secret access
	wg := multierror.Group{}
	if len(clusters) == 0 {
		clusters = t.Clusters()
	}
	for _, c := range clusters {
		c := c
		wg.Go(func() error {
			roleBinding := &rbacv1.RoleBinding{
				TypeMeta: metav1.TypeMeta{
					Kind:       "RoleBinding",
					APIVersion: "rbac.authorization.k8s.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      roleName,
					Namespace: namespace,
				},
				Subjects: []rbacv1.Subject{
					rbacv1.Subject{
						Kind:      "ServiceAccount",
						Namespace: namespace,
						Name:      subject,
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "Role",
					Name:     roleName,
				},
			}
			_, err := c.Kube().RbacV1().RoleBindings(namespace).Create(context.TODO(), roleBinding, metav1.CreateOptions{})
			if err != nil {
				if errors.IsAlreadyExists(err) {
					if _, err := c.Kube().RbacV1().RoleBindings(namespace).Update(context.TODO(), roleBinding, metav1.UpdateOptions{}); err != nil {
						return fmt.Errorf("failed to update rolebindings (error: %s)", err)
					}
				} else {
					return fmt.Errorf("failed to update rolebindings (error: %s)", err)
				}
			}
			// Check if Kubernetes role is ready
			return retry.UntilSuccess(func() error {
				_, err := c.Kube().RbacV1().RoleBindings(namespace).Get(context.TODO(), "test", metav1.GetOptions{})
				if err != nil {
					return fmt.Errorf("rolebinding %v not found: %v", "test", err)
				}
				return nil
			}, retry.Timeout(time.Second*5))
		})
	}
	if err := wg.Wait().ErrorOrNil(); err != nil {
		t.Fatal(err)
	}
}
