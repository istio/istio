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

package validation

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	extensions "istio.io/api/extensions/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	networkingv1beta1 "istio.io/api/networking/v1beta1"
	security_beta "istio.io/api/security/v1beta1"
	telemetry "istio.io/api/telemetry/v1alpha1"
	api "istio.io/api/type/v1beta1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test/util/assert"
)

const (
	// Config name for testing
	someName = "foo"
	// Config namespace for testing.
	someNamespace = "bar"
)

func TestValidateMaxServerConnectionAge(t *testing.T) {
	type durationCheck struct {
		duration time.Duration
		isValid  bool
	}
	durMin, _ := time.ParseDuration("-30m")
	durHr, _ := time.ParseDuration("-1.5h")
	checks := []durationCheck{
		{
			duration: 30 * time.Minute,
			isValid:  true,
		},
		{
			duration: durMin,
			isValid:  false,
		},
		{
			duration: durHr,
			isValid:  false,
		},
	}

	for _, check := range checks {
		if got := ValidateMaxServerConnectionAge(check.duration); (got == nil) != check.isValid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for %v", got == nil, check.isValid, got, check.duration)
		}
	}
}

func TestValidateGateway(t *testing.T) {
	tests := []struct {
		name    string
		in      proto.Message
		out     string
		warning string
	}{
		{"empty", &networking.Gateway{}, "server", ""},
		{"invalid message", &networking.Server{}, "cannot cast", ""},
		{
			"happy domain",
			&networking.Gateway{
				Servers: []*networking.Server{{
					Hosts: []string{"foo.bar.com"},
					Port:  &networking.Port{Name: "name1", Number: 7, Protocol: "http"},
				}},
			},
			"", "",
		},
		{
			"happy multiple servers",
			&networking.Gateway{
				Servers: []*networking.Server{
					{
						Hosts: []string{"foo.bar.com"},
						Port:  &networking.Port{Name: "name1", Number: 7, Protocol: "http"},
					},
				},
			},
			"", "",
		},
		{
			"happy k8s gateway-api server with no attached routes",
			&networking.Gateway{
				Servers: []*networking.Server{{
					Hosts: []string{"~/foo.bar.com"},
					Port:  &networking.Port{Name: "name1", Number: 7, Protocol: "http"},
				}},
			},
			"invalid namespace value", "",
		},
		{
			"invalid port",
			&networking.Gateway{
				Servers: []*networking.Server{
					{
						Hosts: []string{"foo.bar.com"},
						Port:  &networking.Port{Name: "name1", Number: 66000, Protocol: "http"},
					},
				},
			},
			"port", "",
		},
		{
			"duplicate port names",
			&networking.Gateway{
				Servers: []*networking.Server{
					{
						Hosts: []string{"foo.bar.com"},
						Port:  &networking.Port{Name: "foo", Number: 80, Protocol: "http"},
					},
					{
						Hosts: []string{"scooby.doo.com"},
						Port:  &networking.Port{Name: "foo", Number: 8080, Protocol: "http"},
					},
				},
			},
			"port names", "",
		},
		{
			"invalid domain",
			&networking.Gateway{
				Servers: []*networking.Server{
					{
						Hosts: []string{"foo.*.bar.com"},
						Port:  &networking.Port{Number: 7, Protocol: "http"},
					},
				},
			},
			"domain", "",
		},
		{
			"valid httpsRedirect",
			&networking.Gateway{
				Servers: []*networking.Server{
					{
						Hosts: []string{"bar.com"},
						Port:  &networking.Port{Name: "http", Number: 80, Protocol: "http"},
						Tls:   &networking.ServerTLSSettings{HttpsRedirect: true},
					},
				},
			},
			"", "",
		},
		{
			"invalid https httpsRedirect",
			&networking.Gateway{
				Servers: []*networking.Server{
					{
						Hosts: []string{"bar.com"},
						Port:  &networking.Port{Name: "https", Number: 80, Protocol: "https"},
						Tls:   &networking.ServerTLSSettings{HttpsRedirect: true},
					},
				},
			},
			"", "tls.httpsRedirect should only be used with http servers",
		},
		{
			"invalid partial wildcard",
			&networking.Gateway{
				Servers: []*networking.Server{
					{
						Hosts: []string{"*bar.com"},
						Port:  &networking.Port{Name: "tls", Number: 443, Protocol: "tls"},
						Tls:   &networking.ServerTLSSettings{Mode: networking.ServerTLSSettings_ISTIO_MUTUAL},
					},
				},
			},
			"partial wildcard \"*bar.com\" not allowed", "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warn, err := ValidateGateway(config.Config{
				Meta: config.Meta{
					Name:      someName,
					Namespace: someNamespace,
				},
				Spec: tt.in,
			})
			checkValidationMessage(t, warn, err, tt.warning, tt.out)
		})
	}
}

func TestValidateK8sGateway(t *testing.T) {
	tests := []struct {
		name    string
		in      proto.Message
		out     string
		warning string
	}{
		{
			"happy k8s gateway-api server with no attached routes",
			&networking.Gateway{
				Servers: []*networking.Server{{
					Hosts: []string{"~/foo.bar.com"},
					Port:  &networking.Port{Name: "name1", Number: 7, Protocol: "http"},
				}},
			},
			"", "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			annotations := map[string]string{}
			annotations[constants.InternalGatewaySemantics] = constants.GatewaySemanticsGateway

			warn, err := ValidateGateway(config.Config{
				Meta: config.Meta{
					Name:        someName,
					Namespace:   someNamespace,
					Annotations: annotations,
				},
				Spec: tt.in,
			})
			checkValidationMessage(t, warn, err, tt.warning, tt.out)
		})
	}
}

func TestValidateServer(t *testing.T) {
	tests := []struct {
		name string
		in   *networking.Server
		out  string
	}{
		{"empty", &networking.Server{}, "host"},
		{"empty", &networking.Server{}, "port"},
		{
			"happy",
			&networking.Server{
				Hosts: []string{"foo.bar.com"},
				Port:  &networking.Port{Number: 7, Name: "http", Protocol: "http"},
			},
			"",
		},
		{
			"happy ip",
			&networking.Server{
				Hosts: []string{"1.1.1.1"},
				Port:  &networking.Port{Number: 7, Name: "http", Protocol: "http"},
			},
			"",
		},
		{
			"happy ns/name",
			&networking.Server{
				Hosts: []string{"ns1/foo.bar.com"},
				Port:  &networking.Port{Number: 7, Name: "http", Protocol: "http"},
			},
			"",
		},
		{
			"happy */name",
			&networking.Server{
				Hosts: []string{"*/foo.bar.com"},
				Port:  &networking.Port{Number: 7, Name: "http", Protocol: "http"},
			},
			"",
		},
		{
			"happy ./name",
			&networking.Server{
				Hosts: []string{"./foo.bar.com"},
				Port:  &networking.Port{Number: 7, Name: "http", Protocol: "http"},
			},
			"",
		},
		{
			"invalid ~/name",
			&networking.Server{
				Hosts: []string{"~/foo.bar.com"},
				Port:  &networking.Port{Number: 7, Name: "http", Protocol: "http"},
			},
			"namespace",
		},
		{
			"invalid domain ns/name format",
			&networking.Server{
				Hosts: []string{"ns1/foo.*.bar.com"},
				Port:  &networking.Port{Number: 7, Name: "http", Protocol: "http"},
			},
			"domain",
		},
		{
			"invalid domain",
			&networking.Server{
				Hosts: []string{"foo.*.bar.com"},
				Port:  &networking.Port{Number: 7, Name: "http", Protocol: "http"},
			},
			"domain",
		},
		{
			"invalid short name host",
			&networking.Server{
				Hosts: []string{"foo"},
				Port:  &networking.Port{Number: 7, Name: "http", Protocol: "http"},
			},
			"short names",
		},
		{
			"invalid port",
			&networking.Server{
				Hosts: []string{"foo.bar.com"},
				Port:  &networking.Port{Number: 66000, Name: "http", Protocol: "http"},
			},
			"port",
		},
		{
			"invalid tls options",
			&networking.Server{
				Hosts: []string{"foo.bar.com"},
				Port:  &networking.Port{Number: 1, Name: "http", Protocol: "http"},
				Tls:   &networking.ServerTLSSettings{Mode: networking.ServerTLSSettings_SIMPLE},
			},
			"TLS",
		},
		{
			"no tls on HTTPS",
			&networking.Server{
				Hosts: []string{"foo.bar.com"},
				Port:  &networking.Port{Number: 10000, Name: "https", Protocol: "https"},
			},
			"must have TLS",
		},
		{
			"tls on HTTP",
			&networking.Server{
				Hosts: []string{"foo.bar.com"},
				Port:  &networking.Port{Number: 10000, Name: "http", Protocol: "http"},
				Tls:   &networking.ServerTLSSettings{Mode: networking.ServerTLSSettings_SIMPLE},
			},
			"cannot have TLS",
		},
		{
			"tls redirect on HTTP",
			&networking.Server{
				Hosts: []string{"foo.bar.com"},
				Port:  &networking.Port{Number: 10000, Name: "http", Protocol: "http"},
				Tls: &networking.ServerTLSSettings{
					HttpsRedirect: true,
				},
			},
			"",
		},
		{
			"bind ip",
			&networking.Server{
				Hosts: []string{"foo.bar.com"},
				Port:  &networking.Port{Number: 7, Name: "http", Protocol: "http"},
				Bind:  "127.0.0.1",
			},
			"",
		},
		{
			"bind unix path with invalid port",
			&networking.Server{
				Hosts: []string{"foo.bar.com"},
				Port:  &networking.Port{Number: 7, Name: "http", Protocol: "http"},
				Bind:  "unix://@foobar",
			},
			"port number must be 0 for unix domain socket",
		},
		{
			"bind unix path",
			&networking.Server{
				Hosts: []string{"foo.bar.com"},
				Port:  &networking.Port{Number: 0, Name: "http", Protocol: "http"},
				Bind:  "unix://@foobar",
			},
			"",
		},
		{
			"bind bad ip",
			&networking.Server{
				Hosts: []string{"foo.bar.com"},
				Port:  &networking.Port{Number: 0, Name: "http", Protocol: "http"},
				Bind:  "foo.bar",
			},
			"foo.bar is not a valid IP",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := validateServer(tt.in, false)
			warn, err := v.Unwrap()
			checkValidationMessage(t, warn, err, "", tt.out)
		})
	}
}

func TestValidateServerPort(t *testing.T) {
	tests := []struct {
		name string
		in   *networking.Port
		bind string
		out  string
	}{
		{"empty", &networking.Port{}, "", "invalid protocol"},
		{"empty", &networking.Port{}, "", "port name"},
		{
			"happy",
			&networking.Port{
				Protocol: "http",
				Number:   1,
				Name:     "Henry",
			},
			"",
			"",
		},
		{
			"invalid protocol",
			&networking.Port{
				Protocol: "kafka",
				Number:   1,
				Name:     "Henry",
			},
			"",
			"invalid protocol",
		},
		{
			"invalid number",
			&networking.Port{
				Protocol: "http",
				Number:   uint32(1 << 30),
				Name:     "http",
			},
			"",
			"port number",
		},
		{
			"name, no number",
			&networking.Port{
				Protocol: "http",
				Number:   0,
				Name:     "Henry",
			},
			"",
			"port number",
		},
		{
			"name, no number, uds",
			&networking.Port{
				Protocol: "http",
				Number:   0,
				Name:     "Henry",
			},
			"uds:///tmp",
			"port number",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := validateServerPort(tt.in, tt.bind)
			_, err := v.Unwrap()
			if err == nil && tt.out != "" {
				t.Fatalf("validateServerPort(%v) = nil, wanted %q", tt.in, tt.out)
			} else if err != nil && tt.out == "" {
				t.Fatalf("validateServerPort(%v) = %v, wanted nil", tt.in, err)
			} else if err != nil && !strings.Contains(err.Error(), tt.out) {
				t.Fatalf("validateServerPort(%v) = %v, wanted %q", tt.in, err, tt.out)
			}
		})
	}
}

func TestValidateTlsOptions(t *testing.T) {
	tests := []struct {
		name    string
		in      *networking.ServerTLSSettings
		out     string
		warning string
	}{
		{"empty", &networking.ServerTLSSettings{}, "", ""},
		{
			"simple",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_SIMPLE,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh",
			},
			"", "",
		},
		{
			"simple with client bundle",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_SIMPLE,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker",
			},
			"", "",
		},
		{
			"simple sds with client bundle",
			&networking.ServerTLSSettings{
				Mode:           networking.ServerTLSSettings_SIMPLE,
				CredentialName: "sds-name",
			},
			"", "",
		},
		{
			"simple sds with client bundle but with server cert",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_SIMPLE,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh",
				CredentialName:    "sds-name",
			},
			"one of credential_name, credential_names", "",
		},
		{
			"simple no server cert",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_SIMPLE,
				ServerCertificate: "",
				PrivateKey:        "Khan Noonien Singh",
			},
			"server certificate", "",
		},
		{
			"simple no private key",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_SIMPLE,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "",
			},
			"private key", "",
		},
		{
			"simple more than 2 certs",
			&networking.ServerTLSSettings{
				Mode: networking.ServerTLSSettings_SIMPLE,
				TlsCertificates: []*networking.ServerTLSSettings_TLSCertificate{
					{
						ServerCertificate: "Captain Jean-Luc Picard",
						PrivateKey:        "Khan Noonien Singh",
					},
					{
						ServerCertificate: "Commander William T. Riker",
						PrivateKey:        "Commander William T. Riker",
					},
					{
						ServerCertificate: "Lieutenant Commander Data",
						PrivateKey:        "Lieutenant Commander Data",
					},
				},
			},
			"", "SIMPLE TLS can support up to 2 server certificates",
		},
		{
			"simple with both server cert and tls certs",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_SIMPLE,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh",
				TlsCertificates: []*networking.ServerTLSSettings_TLSCertificate{
					{
						ServerCertificate: "Captain Jean-Luc Picard",
						PrivateKey:        "Khan Noonien Singh",
					},
					{
						ServerCertificate: "Lieutenant Commander Data",
						PrivateKey:        "Lieutenant Commander Data",
					},
				},
			},
			"one of credential_name, credential_names", "",
		},
		{
			"simple 2 certs with missing certificate",
			&networking.ServerTLSSettings{
				Mode: networking.ServerTLSSettings_SIMPLE,
				TlsCertificates: []*networking.ServerTLSSettings_TLSCertificate{
					{
						PrivateKey: "Commander William T. Riker",
					},
					{
						ServerCertificate: "Commander William T. Riker",
						PrivateKey:        "Commander William T. Riker",
					},
				},
			},
			"server certificate", "",
		},
		{
			"simple 2 certs with missing private key",
			&networking.ServerTLSSettings{
				Mode: networking.ServerTLSSettings_SIMPLE,
				TlsCertificates: []*networking.ServerTLSSettings_TLSCertificate{
					{
						ServerCertificate: "Captain Jean-Luc Picard",
					},
					{
						ServerCertificate: "Commander William T. Riker",
						PrivateKey:        "Commander William T. Riker",
					},
				},
			},
			"private key", "",
		},
		{
			"simple sds no server cert",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_SIMPLE,
				ServerCertificate: "",
				PrivateKey:        "Khan Noonien Singh",
				CredentialName:    "sds-name",
			},
			"", "",
		},
		{
			"simple sds no private key",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_SIMPLE,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "",
				CredentialName:    "sds-name",
			},
			"", "",
		},
		{
			"mutual",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker",
			},
			"", "",
		},
		{
			"mutual sds",
			&networking.ServerTLSSettings{
				Mode:           networking.ServerTLSSettings_MUTUAL,
				CaCertificates: "Commander William T. Riker",
				CredentialName: "sds-name",
			},
			"", "",
		},
		{
			"mutual sds with credential name and server cert",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker",
				CredentialName:    "sds-name",
			},
			"one of credential_name, credential_names", "",
		},
		{
			"mutual sds with credential name and tls certs",
			&networking.ServerTLSSettings{
				Mode:           networking.ServerTLSSettings_MUTUAL,
				CaCertificates: "Commander William T. Riker",
				TlsCertificates: []*networking.ServerTLSSettings_TLSCertificate{
					{
						ServerCertificate: "Captain Jean-Luc Picard",
						PrivateKey:        "Khan Noonien Singh",
					},
				},
				CredentialName: "sds-name",
			},
			"one of credential_name, credential_names", "",
		},
		{
			"mutual no server cert",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker",
			},
			"server certificate", "",
		},
		{
			"credential names and certificates",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				ServerCertificate: "/etc/istio/certs/server/cert",
				PrivateKey:        "/etc/istio/certs/server/key",
				CredentialNames:   []string{"server-certs"},
			},
			"one of credential_name, credential_names", "",
		},
		{
			"credential names and tls certificates",
			&networking.ServerTLSSettings{
				Mode: networking.ServerTLSSettings_MUTUAL,
				TlsCertificates: []*networking.ServerTLSSettings_TLSCertificate{
					{
						ServerCertificate: "/etc/istio/certs/server/cert",
						PrivateKey:        "/etc/istio/certs/server/key",
						CaCertificates:    "/etc/istio/certs/server/cacert2",
					},
				},
				CredentialNames: []string{"server-certs"},
			},
			"one of credential_name, credential_names", "",
		},
		{
			"only tls certificates",
			&networking.ServerTLSSettings{
				Mode: networking.ServerTLSSettings_MUTUAL,
				TlsCertificates: []*networking.ServerTLSSettings_TLSCertificate{
					{
						ServerCertificate: "/etc/istio/certs/server/cert",
						PrivateKey:        "/etc/istio/certs/server/key",
					},
				},
				CaCertificates: "/etc/istio/certs/server/cacert2",
			},
			"", "",
		},
		{
			"mutual sds no server cert",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker",
				CredentialName:    "sds-name",
			},
			"", "",
		},
		{
			"mutual no client CA bundle",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "",
			},
			"client CA bundle", "",
		},
		// this pair asserts we get errors about both client and server certs missing when in mutual mode
		// and both are absent, but requires less rewriting of the testing harness than merging the cases
		{
			"mutual no certs",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "",
				CaCertificates:    "",
			},
			"server certificate", "",
		},
		{
			"mutual no certs",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "",
				CaCertificates:    "",
			},
			"private key", "",
		},
		{
			"mutual no certs",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "",
				CaCertificates:    "",
			},
			"client CA bundle", "",
		},
		{
			"mutual more than 2 certs",
			&networking.ServerTLSSettings{
				Mode:           networking.ServerTLSSettings_MUTUAL,
				CaCertificates: "Commander William T. Riker",
				TlsCertificates: []*networking.ServerTLSSettings_TLSCertificate{
					{
						ServerCertificate: "Captain Jean-Luc Picard",
						PrivateKey:        "Khan Noonien Singh",
					},
					{
						ServerCertificate: "Commander William T. Riker",
						PrivateKey:        "Commander William T. Riker",
					},
					{
						ServerCertificate: "Lieutenant Commander Data",
						PrivateKey:        "Lieutenant Commander Data",
					},
				},
			},
			"", "MUTUAL TLS can support up to 2 server certificates",
		},
		{
			"mutual with both server cert and tls certs",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker",
				TlsCertificates: []*networking.ServerTLSSettings_TLSCertificate{
					{
						ServerCertificate: "Captain Jean-Luc Picard",
						PrivateKey:        "Khan Noonien Singh",
					},
					{
						ServerCertificate: "Lieutenant Commander Data",
						PrivateKey:        "Lieutenant Commander Data",
					},
				},
			},
			"one of credential_name, credential_names", "",
		},
		{
			"mutual 2 certs with missing certificate",
			&networking.ServerTLSSettings{
				Mode:           networking.ServerTLSSettings_MUTUAL,
				CaCertificates: "Lieutenant Commander Data",
				TlsCertificates: []*networking.ServerTLSSettings_TLSCertificate{
					{
						PrivateKey: "Commander William T. Riker",
					},
					{
						ServerCertificate: "Commander William T. Riker",
						PrivateKey:        "Commander William T. Riker",
					},
				},
			},
			"server certificate", "",
		},
		{
			"mutual 2 certs with missing private key",
			&networking.ServerTLSSettings{
				Mode:           networking.ServerTLSSettings_MUTUAL,
				CaCertificates: "Lieutenant Commander Data",
				TlsCertificates: []*networking.ServerTLSSettings_TLSCertificate{
					{
						ServerCertificate: "Captain Jean-Luc Picard",
					},
					{
						ServerCertificate: "Commander William T. Riker",
						PrivateKey:        "Commander William T. Riker",
					},
				},
			},
			"private key", "",
		},
		{
			"mutual 2 certs with missing CA certificate",
			&networking.ServerTLSSettings{
				Mode: networking.ServerTLSSettings_MUTUAL,
				TlsCertificates: []*networking.ServerTLSSettings_TLSCertificate{
					{
						ServerCertificate: "Captain Jean-Luc Picard",
						PrivateKey:        "Khan Noonien Singh",
					},
					{
						ServerCertificate: "Commander William T. Riker",
						PrivateKey:        "Commander William T. Riker",
					},
				},
			},
			"client CA bundle", "",
		},
		{
			"optional mutual no certs",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_OPTIONAL_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "",
				CaCertificates:    "",
			},
			"server certificate", "",
		},
		{
			"optional mutual no certs",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_OPTIONAL_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "",
				CaCertificates:    "",
			},
			"private key", "",
		},
		{
			"optional mutual no certs",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_OPTIONAL_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "",
				CaCertificates:    "",
			},
			"client CA bundle", "",
		},
		{
			"pass through sds no certs",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_PASSTHROUGH,
				ServerCertificate: "",
				CaCertificates:    "",
				CredentialName:    "sds-name",
			},
			"", "PASSTHROUGH mode does not use certificates",
		},
		{
			"pass through sds crl",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_PASSTHROUGH,
				ServerCertificate: "",
				CaCertificates:    "",
				CaCrl:             "scrl",
			},
			"", "PASSTHROUGH mode does not use certificates",
		},
		{
			"istio_mutual no certs",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_ISTIO_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "",
				CaCertificates:    "",
			},
			"", "",
		},
		{
			"istio_mutual with server cert",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_ISTIO_MUTUAL,
				ServerCertificate: "Captain Jean-Luc Picard",
			},
			"cannot have associated server cert", "",
		},
		{
			"istio_mutual with client bundle",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_ISTIO_MUTUAL,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker",
			},
			"cannot have associated", "",
		},
		{
			"istio_mutual with private key",
			&networking.ServerTLSSettings{
				Mode:       networking.ServerTLSSettings_ISTIO_MUTUAL,
				PrivateKey: "Khan Noonien Singh",
			},
			"cannot have associated private key", "",
		},
		{
			"istio_mutual with credential name",
			&networking.ServerTLSSettings{
				Mode:           networking.ServerTLSSettings_ISTIO_MUTUAL,
				CredentialName: "some-cred",
			},
			"cannot have associated credentialName", "",
		},
		{
			"invalid cipher suites",
			&networking.ServerTLSSettings{
				Mode:           networking.ServerTLSSettings_SIMPLE,
				CredentialName: "sds-name",
				CipherSuites:   []string{"not-a-cipher-suite"},
			},
			"", "not-a-cipher-suite",
		},
		{
			"valid cipher suites",
			&networking.ServerTLSSettings{
				Mode:           networking.ServerTLSSettings_SIMPLE,
				CredentialName: "sds-name",
				CipherSuites:   []string{"ECDHE-ECDSA-AES128-SHA"},
			},
			"", "",
		},
		{
			"cipher suites operations",
			&networking.ServerTLSSettings{
				Mode:           networking.ServerTLSSettings_SIMPLE,
				CredentialName: "sds-name",
				CipherSuites:   []string{"-ECDHE-ECDSA-AES128-SHA"},
			},
			"", "",
		},
		{
			"duplicate cipher suites",
			&networking.ServerTLSSettings{
				Mode:           networking.ServerTLSSettings_SIMPLE,
				CredentialName: "sds-name",
				CipherSuites:   []string{"ECDHE-ECDSA-AES128-SHA", "ECDHE-ECDSA-AES128-SHA"},
			},
			"", "ECDHE-ECDSA-AES128-SHA",
		},
		{
			"invalid cipher suites with invalid config",
			&networking.ServerTLSSettings{
				Mode:         networking.ServerTLSSettings_SIMPLE,
				CipherSuites: []string{"not-a-cipher-suite"},
			},
			"requires a private key", "not-a-cipher-suite",
		},
		{
			"crl specified for SIMPLE TLS",
			&networking.ServerTLSSettings{
				Mode:  networking.ServerTLSSettings_SIMPLE,
				CaCrl: "crl",
			},
			"CRL is not supported with SIMPLE TLS", "",
		},
		{
			"crl specified for CredentialName",
			&networking.ServerTLSSettings{
				Mode:           networking.ServerTLSSettings_SIMPLE,
				CaCrl:          "crl",
				CredentialName: "credential",
			},
			"", "",
		},
		{
			"mutual no server cert",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker",
			},
			"MUTUAL TLS requires a server certificate", "",
		},
		{
			"mutual no private key",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				ServerCertificate: "Khan Noonien Singh",
				PrivateKey:        "",
				CaCertificates:    "Commander William T. Riker",
			},
			"MUTUAL TLS requires a private key", "",
		},
		{
			"with CredentialNames",
			&networking.ServerTLSSettings{
				Mode:            networking.ServerTLSSettings_MUTUAL,
				CredentialNames: []string{"credential1", "credential2"},
			},
			"", "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := validateTLSOptions(tt.in)
			warn, err := v.Unwrap()
			checkValidationMessage(t, warn, err, tt.warning, tt.out)
		})
	}
}

func TestStrictValidateHTTPHeaderName(t *testing.T) {
	testCases := []struct {
		name  string
		valid bool
	}{
		{name: "header1", valid: true},
		{name: "X-Requested-With", valid: true},
		{name: ":authority", valid: false},
		{name: "", valid: false},
	}

	for _, tc := range testCases {
		if got := ValidateStrictHTTPHeaderName(tc.name); (got == nil) != tc.valid {
			t.Errorf("ValidateStrictHTTPHeaderName(%q) => got valid=%v, want valid=%v",
				tc.name, got == nil, tc.valid)
		}
	}
}

func TestValidateHTTPHeaderName(t *testing.T) {
	testCases := []struct {
		name  string
		valid bool
	}{
		{name: "header1", valid: true},
		{name: "X-Requested-With", valid: true},
		{name: ":authority", valid: true},
		{name: "funky!$#_", valid: true},
		{name: "", valid: false},
		{name: "::", valid: false},
		{name: "illegal\"char", valid: false},
	}

	for _, tc := range testCases {
		if got := ValidateHTTPHeaderNameOrJwtClaimRoute(tc.name); (got == nil) != tc.valid {
			t.Errorf("ValidateStrictHTTPHeaderName(%q) => got valid=%v, want valid=%v",
				tc.name, got == nil, tc.valid)
		}
	}
}

func TestValidateCORSPolicy(t *testing.T) {
	testCases := []struct {
		name  string
		in    *networking.CorsPolicy
		valid bool
	}{
		{name: "valid", in: &networking.CorsPolicy{
			AllowMethods:  []string{"GET", "POST"},
			AllowHeaders:  []string{"header1", "header2"},
			ExposeHeaders: []string{"header3"},
			MaxAge:        &durationpb.Duration{Seconds: 2},
		}, valid: true},
		{name: "bad method", in: &networking.CorsPolicy{
			AllowMethods:  []string{"GET", "PUTT"},
			AllowHeaders:  []string{"header1", "header2"},
			ExposeHeaders: []string{"header3"},
			MaxAge:        &durationpb.Duration{Seconds: 2},
		}, valid: false},
		{name: "bad header", in: &networking.CorsPolicy{
			AllowMethods:  []string{"GET", "POST"},
			AllowHeaders:  []string{"header1", "header2"},
			ExposeHeaders: []string{""},
			MaxAge:        &durationpb.Duration{Seconds: 2},
		}, valid: false},
		{name: "bad max age", in: &networking.CorsPolicy{
			AllowMethods:  []string{"GET", "POST"},
			AllowHeaders:  []string{"header1", "header2"},
			ExposeHeaders: []string{"header3"},
			MaxAge:        &durationpb.Duration{Seconds: 2, Nanos: 42},
		}, valid: false},
		{name: "empty matchType AllowOrigins", in: &networking.CorsPolicy{
			AllowOrigins: []*networking.StringMatch{
				{MatchType: &networking.StringMatch_Exact{Exact: ""}},
				{MatchType: &networking.StringMatch_Prefix{Prefix: ""}},
				{MatchType: &networking.StringMatch_Regex{Regex: ""}},
			},
			AllowMethods:  []string{"GET", "POST"},
			AllowHeaders:  []string{"header1", "header2"},
			ExposeHeaders: []string{"header3"},
			MaxAge:        &durationpb.Duration{Seconds: 2},
		}, valid: false},
		{name: "non empty matchType AllowOrigins", in: &networking.CorsPolicy{
			AllowOrigins: []*networking.StringMatch{
				{MatchType: &networking.StringMatch_Exact{Exact: "exact"}},
				{MatchType: &networking.StringMatch_Prefix{Prefix: "prefix"}},
				{MatchType: &networking.StringMatch_Regex{Regex: "regex"}},
			},
			AllowMethods:  []string{"GET", "POST"},
			AllowHeaders:  []string{"header1", "header2"},
			ExposeHeaders: []string{"header3"},
			MaxAge:        &durationpb.Duration{Seconds: 2},
		}, valid: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateCORSPolicy(tc.in); (got == nil) != tc.valid {
				t.Errorf("got valid=%v, want valid=%v: %v",
					got == nil, tc.valid, got)
			}
		})
	}
}

func TestValidateHTTPStatus(t *testing.T) {
	testCases := []struct {
		in    int32
		valid bool
	}{
		{-100, false},
		{0, false},
		{200, true},
		{600, true},
		{601, false},
	}

	for _, tc := range testCases {
		if got := validateHTTPStatus(tc.in); (got == nil) != tc.valid {
			t.Errorf("validateHTTPStatus(%d) => got valid=%v, want valid=%v",
				tc.in, got, tc.valid)
		}
	}
}

func TestValidateHTTPFaultInjectionAbort(t *testing.T) {
	testCases := []struct {
		name  string
		in    *networking.HTTPFaultInjection_Abort
		valid bool
	}{
		{name: "nil", in: nil, valid: true},
		{name: "valid", in: &networking.HTTPFaultInjection_Abort{
			Percentage: &networking.Percent{
				Value: 20,
			},
			ErrorType: &networking.HTTPFaultInjection_Abort_HttpStatus{
				HttpStatus: 200,
			},
		}, valid: true},
		{name: "valid default", in: &networking.HTTPFaultInjection_Abort{
			ErrorType: &networking.HTTPFaultInjection_Abort_HttpStatus{
				HttpStatus: 200,
			},
		}, valid: true},
		{name: "invalid http status", in: &networking.HTTPFaultInjection_Abort{
			Percentage: &networking.Percent{
				Value: 20,
			},
			ErrorType: &networking.HTTPFaultInjection_Abort_HttpStatus{
				HttpStatus: 9000,
			},
		}, valid: false},
		{name: "invalid low http status", in: &networking.HTTPFaultInjection_Abort{
			Percentage: &networking.Percent{
				Value: 20,
			},
			ErrorType: &networking.HTTPFaultInjection_Abort_HttpStatus{
				HttpStatus: 100,
			},
		}, valid: false},
		{name: "valid percentage", in: &networking.HTTPFaultInjection_Abort{
			Percentage: &networking.Percent{
				Value: 0.001,
			},
			ErrorType: &networking.HTTPFaultInjection_Abort_HttpStatus{
				HttpStatus: 200,
			},
		}, valid: true},
		{name: "invalid fractional percent", in: &networking.HTTPFaultInjection_Abort{
			Percentage: &networking.Percent{
				Value: -10.0,
			},
			ErrorType: &networking.HTTPFaultInjection_Abort_HttpStatus{
				HttpStatus: 200,
			},
		}, valid: false},
		{name: "grpc: nil", in: nil, valid: true},
		{name: "grpc: valid", in: &networking.HTTPFaultInjection_Abort{
			Percentage: &networking.Percent{
				Value: 20,
			},
			ErrorType: &networking.HTTPFaultInjection_Abort_GrpcStatus{
				GrpcStatus: "DEADLINE_EXCEEDED",
			},
		}, valid: true},
		{name: "grpc: valid default percentage", in: &networking.HTTPFaultInjection_Abort{
			ErrorType: &networking.HTTPFaultInjection_Abort_GrpcStatus{
				GrpcStatus: "DEADLINE_EXCEEDED",
			},
		}, valid: true},
		{name: "grpc: invalid status", in: &networking.HTTPFaultInjection_Abort{
			Percentage: &networking.Percent{
				Value: 20,
			},
			ErrorType: &networking.HTTPFaultInjection_Abort_GrpcStatus{
				GrpcStatus: "BAD_STATUS",
			},
		}, valid: false},
		{name: "grpc: valid percentage", in: &networking.HTTPFaultInjection_Abort{
			Percentage: &networking.Percent{
				Value: 0.001,
			},
			ErrorType: &networking.HTTPFaultInjection_Abort_GrpcStatus{
				GrpcStatus: "INTERNAL",
			},
		}, valid: true},
		{name: "grpc: invalid fractional percent", in: &networking.HTTPFaultInjection_Abort{
			Percentage: &networking.Percent{
				Value: -10.0,
			},
			ErrorType: &networking.HTTPFaultInjection_Abort_GrpcStatus{
				GrpcStatus: "DEADLINE_EXCEEDED",
			},
		}, valid: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateHTTPFaultInjectionAbort(tc.in); (got.Err == nil) != tc.valid {
				t.Errorf("got valid=%v, want valid=%v: %v",
					got.Err == nil, tc.valid, got)
			}
		})
	}
}

func TestValidateHTTPFaultInjectionDelay(t *testing.T) {
	testCases := []struct {
		name  string
		in    *networking.HTTPFaultInjection_Delay
		valid bool
	}{
		{name: "nil", in: nil, valid: true},
		{name: "valid fixed", in: &networking.HTTPFaultInjection_Delay{
			Percentage: &networking.Percent{
				Value: 20,
			},
			HttpDelayType: &networking.HTTPFaultInjection_Delay_FixedDelay{
				FixedDelay: &durationpb.Duration{Seconds: 3},
			},
		}, valid: true},
		{name: "valid default", in: &networking.HTTPFaultInjection_Delay{
			HttpDelayType: &networking.HTTPFaultInjection_Delay_FixedDelay{
				FixedDelay: &durationpb.Duration{Seconds: 3},
			},
		}, valid: true},
		{name: "invalid percent", in: &networking.HTTPFaultInjection_Delay{
			Percentage: &networking.Percent{
				Value: 101,
			},
			HttpDelayType: &networking.HTTPFaultInjection_Delay_FixedDelay{
				FixedDelay: &durationpb.Duration{Seconds: 3},
			},
		}, valid: false},
		{name: "invalid delay", in: &networking.HTTPFaultInjection_Delay{
			Percentage: &networking.Percent{
				Value: 20,
			},
			HttpDelayType: &networking.HTTPFaultInjection_Delay_FixedDelay{
				FixedDelay: &durationpb.Duration{Seconds: 3, Nanos: 42},
			},
		}, valid: false},
		{name: "valid fractional percentage", in: &networking.HTTPFaultInjection_Delay{
			Percentage: &networking.Percent{
				Value: 0.001,
			},
			HttpDelayType: &networking.HTTPFaultInjection_Delay_FixedDelay{
				FixedDelay: &durationpb.Duration{Seconds: 3},
			},
		}, valid: true},
		{name: "invalid fractional percentage", in: &networking.HTTPFaultInjection_Delay{
			Percentage: &networking.Percent{
				Value: -10.0,
			},
			HttpDelayType: &networking.HTTPFaultInjection_Delay_FixedDelay{
				FixedDelay: &durationpb.Duration{Seconds: 3},
			},
		}, valid: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateHTTPFaultInjectionDelay(tc.in); (got == nil) != tc.valid {
				t.Errorf("got valid=%v, want valid=%v: %v",
					got == nil, tc.valid, got)
			}
		})
	}
}

func TestValidateHTTPRetry(t *testing.T) {
	testCases := []struct {
		name  string
		in    *networking.HTTPRetry
		valid bool
	}{
		{name: "valid", in: &networking.HTTPRetry{
			Attempts:      10,
			PerTryTimeout: &durationpb.Duration{Seconds: 2},
			RetryOn:       "5xx,gateway-error",
		}, valid: true},
		{name: "disable retries", in: &networking.HTTPRetry{
			Attempts: 0,
		}, valid: true},
		{name: "invalid, retry policy configured but attempts set to zero", in: &networking.HTTPRetry{
			Attempts:      0,
			PerTryTimeout: &durationpb.Duration{Seconds: 2},
			RetryOn:       "5xx,gateway-error",
		}, valid: false},
		{name: "valid default", in: &networking.HTTPRetry{
			Attempts: 10,
		}, valid: true},
		{name: "valid http status retryOn", in: &networking.HTTPRetry{
			Attempts:      10,
			PerTryTimeout: &durationpb.Duration{Seconds: 2},
			RetryOn:       "503,connect-failure",
		}, valid: true},
		{name: "invalid attempts", in: &networking.HTTPRetry{
			Attempts:      -1,
			PerTryTimeout: &durationpb.Duration{Seconds: 2},
		}, valid: false},
		{name: "invalid timeout", in: &networking.HTTPRetry{
			Attempts:      10,
			PerTryTimeout: &durationpb.Duration{Seconds: 2, Nanos: 1},
		}, valid: false},
		{name: "timeout too small", in: &networking.HTTPRetry{
			Attempts:      10,
			PerTryTimeout: &durationpb.Duration{Nanos: 999},
		}, valid: false},
		{name: "invalid backoff", in: &networking.HTTPRetry{
			Attempts: 10,
			Backoff:  &durationpb.Duration{Nanos: 999},
		}, valid: false},
		{name: "invalid policy retryOn", in: &networking.HTTPRetry{
			Attempts:      10,
			PerTryTimeout: &durationpb.Duration{Seconds: 2},
			RetryOn:       "5xx,invalid policy",
		}, valid: false},
		{name: "invalid http status retryOn", in: &networking.HTTPRetry{
			Attempts:      10,
			PerTryTimeout: &durationpb.Duration{Seconds: 2},
			RetryOn:       "600,connect-failure",
		}, valid: false},
		{name: "invalid, retryRemoteLocalities configured but attempts set to zero", in: &networking.HTTPRetry{
			Attempts:              0,
			RetryRemoteLocalities: &wrapperspb.BoolValue{Value: false},
		}, valid: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateHTTPRetry(tc.in); (got == nil) != tc.valid {
				t.Errorf("got valid=%v, want valid=%v: %v",
					got == nil, tc.valid, got)
			}
		})
	}
}

func TestValidateHTTPRewrite(t *testing.T) {
	testCases := []struct {
		name  string
		in    *networking.HTTPRewrite
		valid bool
	}{
		{
			name:  "nil in",
			in:    nil,
			valid: true,
		},
		{
			name: "uri and authority",
			in: &networking.HTTPRewrite{
				Uri:       "/path/to/resource",
				Authority: "foobar.org",
			},
			valid: true,
		},
		{
			name: "uri",
			in: &networking.HTTPRewrite{
				Uri: "/path/to/resource",
			},
			valid: true,
		},
		{
			name: "authority",
			in: &networking.HTTPRewrite{
				Authority: "foobar.org",
			},
			valid: true,
		},
		{
			name: "uriRegexRewrite",
			in: &networking.HTTPRewrite{
				UriRegexRewrite: &networking.RegexRewrite{
					Match:   "^/service/([^/]+)(/.*)$",
					Rewrite: `\2/instance/\1`,
				},
			},
			valid: true,
		},
		{
			name: "uriRegexRewrite and authority",
			in: &networking.HTTPRewrite{
				Authority: "foobar.org",
				UriRegexRewrite: &networking.RegexRewrite{
					Match:   "^/service/([^/]+)(/.*)$",
					Rewrite: `\2/instance/\1`,
				},
			},
			valid: true,
		},
		{
			name: "uriRegexRewrite and uri",
			in: &networking.HTTPRewrite{
				Uri: "/path/to/resource",
				UriRegexRewrite: &networking.RegexRewrite{
					Match:   "^/service/([^/]+)(/.*)$",
					Rewrite: `\2/instance/\1`,
				},
			},
			valid: false,
		},
		{
			name:  "no uri, uriRegexRewrite, or authority",
			in:    &networking.HTTPRewrite{},
			valid: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateHTTPRewrite(tc.in); (got == nil) != tc.valid {
				t.Errorf("got valid=%v, want valid=%v: %v",
					got == nil, tc.valid, got)
			}
		})
	}
}

func TestValidateUriRegexRewrite(t *testing.T) {
	testCases := []struct {
		name  string
		in    *networking.RegexRewrite
		valid bool
	}{
		{
			name:  "uriRegexRewrite nil",
			in:    nil,
			valid: true,
		},
		{
			name: "uriRegexRewrite happy path",
			in: &networking.RegexRewrite{
				Match:   "^/service/([^/]+)(/.*)$",
				Rewrite: `\2/instance/\1`,
			},
			valid: true,
		},
		{
			name: "uriRegexRewrite missing match",
			in: &networking.RegexRewrite{
				Rewrite: `\2/instance/\1`,
			},
			valid: false,
		},
		{
			name: "uriRegexRewrite missing rewrite",
			in: &networking.RegexRewrite{
				Match: "^/service/([^/]+)(/.*)$",
			},
			valid: false,
		},
		{
			name: "uriRegexRewrite invalid regex patterns",
			in: &networking.RegexRewrite{
				Match:   "[",
				Rewrite: "[",
			},
			valid: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateURIRegexRewrite(tc.in); (got == nil) != tc.valid {
				t.Errorf("got valid=%v, want valid=%v: %v",
					got == nil, tc.valid, got)
			}
		})
	}
}

func TestValidatePortName(t *testing.T) {
	testCases := []struct {
		name  string
		valid bool
	}{
		{
			name:  "",
			valid: false,
		},
		{
			name:  "simple",
			valid: true,
		},
		{
			name:  "full",
			valid: true,
		},
		{
			name:  "toolongtoolongtoolongtoolongtoolongtoolongtoolongtoolongtoolongtoolongtoolongtoolongtoolongtoolong",
			valid: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidatePortName(tc.name); (err == nil) != tc.valid {
				t.Fatalf("got valid=%v but wanted valid=%v: %v", err == nil, tc.valid, err)
			}
		})
	}
}

func TestValidateHTTPRedirect(t *testing.T) {
	testCases := []struct {
		name     string
		redirect *networking.HTTPRedirect
		valid    bool
	}{
		{
			name:     "nil redirect",
			redirect: nil,
			valid:    true,
		},
		{
			name: "empty uri and authority",
			redirect: &networking.HTTPRedirect{
				Uri:       "",
				Authority: "",
			},
			valid: false,
		},
		{
			name: "too small redirect code",
			redirect: &networking.HTTPRedirect{
				Uri:          "t",
				Authority:    "",
				RedirectCode: 299,
			},
			valid: false,
		},
		{
			name: "too large redirect code",
			redirect: &networking.HTTPRedirect{
				Uri:          "t",
				Authority:    "",
				RedirectCode: 400,
			},
			valid: false,
		},
		{
			name: "empty authority",
			redirect: &networking.HTTPRedirect{
				Uri:       "t",
				Authority: "",
			},
			valid: true,
		},
		{
			name: "empty uri",
			redirect: &networking.HTTPRedirect{
				Uri:       "",
				Authority: "t",
			},
			valid: true,
		},
		{
			name: "empty redirect code",
			redirect: &networking.HTTPRedirect{
				Uri:          "t",
				Authority:    "t",
				RedirectCode: 0,
			},
			valid: true,
		},
		{
			name: "normal redirect",
			redirect: &networking.HTTPRedirect{
				Uri:          "t",
				Authority:    "t",
				RedirectCode: 308,
			},
			valid: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateHTTPRedirect(tc.redirect); (err == nil) != tc.valid {
				t.Fatalf("got valid=%v but wanted valid=%v: %v", err == nil, tc.valid, err)
			}
		})
	}
}

func TestValidateHTTPDirectResponse(t *testing.T) {
	testCases := []struct {
		name           string
		directResponse *networking.HTTPDirectResponse
		valid          bool
		warning        bool
	}{
		{
			name:           "nil redirect",
			directResponse: nil,
			valid:          true,
		},
		{
			name: "status 200",
			directResponse: &networking.HTTPDirectResponse{
				Status: 200,
			},
			valid: true,
		},
		{
			name: "status 100",
			directResponse: &networking.HTTPDirectResponse{
				Status: 199,
			},
			valid: false,
		},
		{
			name: "status 600",
			directResponse: &networking.HTTPDirectResponse{
				Status: 601,
			},
			valid: false,
		},
		{
			name: "with string body",
			directResponse: &networking.HTTPDirectResponse{
				Status: 200,
				Body: &networking.HTTPBody{
					Specifier: &networking.HTTPBody_String_{String_: "hello"},
				},
			},
			valid: true,
		},
		{
			name: "with string body over 100kb",
			directResponse: &networking.HTTPDirectResponse{
				Status: 200,
				Body: &networking.HTTPBody{
					Specifier: &networking.HTTPBody_String_{String_: strings.Repeat("a", 101*kb)},
				},
			},
			valid:   true,
			warning: true,
		},
		{
			name: "with string body over 1mb",
			directResponse: &networking.HTTPDirectResponse{
				Status: 200,
				Body: &networking.HTTPBody{
					Specifier: &networking.HTTPBody_String_{String_: strings.Repeat("a", 2*mb)},
				},
			},
			valid: false,
		},
		{
			name: "with bytes body",
			directResponse: &networking.HTTPDirectResponse{
				Status: 200,
				Body: &networking.HTTPBody{
					Specifier: &networking.HTTPBody_Bytes{Bytes: []byte("hello")},
				},
			},
			valid: true,
		},
		{
			name: "with bytes body over 100kb",
			directResponse: &networking.HTTPDirectResponse{
				Status: 200,
				Body: &networking.HTTPBody{
					Specifier: &networking.HTTPBody_Bytes{Bytes: []byte(strings.Repeat("a", (100*kb)+1))},
				},
			},
			valid:   true,
			warning: true,
		},
		{
			name: "with bytes body over 1mb",
			directResponse: &networking.HTTPDirectResponse{
				Status: 200,
				Body: &networking.HTTPBody{
					Specifier: &networking.HTTPBody_Bytes{Bytes: []byte(strings.Repeat("a", (1*mb)+1))},
				},
			},
			valid: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateHTTPDirectResponse(tc.directResponse); (err.Err == nil) != tc.valid {
				t.Fatalf("got valid=%v but wanted valid=%v: %v", err.Err == nil, tc.valid, err)
			}
			if err := validateHTTPDirectResponse(tc.directResponse); (err.Warning != nil) != tc.warning {
				t.Fatalf("got valid=%v but wanted valid=%v: %v", err.Warning != nil, tc.warning, err)
			}
		})
	}
}

func TestValidateDestination(t *testing.T) {
	testCases := []struct {
		name        string
		destination *networking.Destination
		valid       bool
	}{
		{
			name:        "empty",
			destination: &networking.Destination{}, // nothing
			valid:       false,
		},
		{
			name: "simple",
			destination: &networking.Destination{
				Host: "foo.bar",
			},
			valid: true,
		},
		{
			name: "full",
			destination: &networking.Destination{
				Host:   "foo.bar",
				Subset: "shiny",
				Port: &networking.PortSelector{
					Number: 5000,
				},
			},
			valid: true,
		},
		{
			name: "unnumbered-selector",
			destination: &networking.Destination{
				Host:   "foo.bar",
				Subset: "shiny",
				Port:   &networking.PortSelector{},
			},
			valid: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateDestination(tc.destination); (err == nil) != tc.valid {
				t.Fatalf("got valid=%v but wanted valid=%v: %v", err == nil, tc.valid, err)
			}
		})
	}
}

func TestValidateHTTPRoute(t *testing.T) {
	testCases := []struct {
		name  string
		route *networking.HTTPRoute
		valid bool
	}{
		{name: "empty", route: &networking.HTTPRoute{ // nothing
		}, valid:                                     false},
		{name: "simple", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
			}},
		}, valid: true},
		{name: "no destination", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: nil,
			}},
		}, valid: false},
		{name: "weighted", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz.south"},
				Weight:      25,
			}, {
				Destination: &networking.Destination{Host: "foo.baz.east"},
				Weight:      75,
			}},
		}, valid: true},
		{name: "total weight > 100", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz.south"},
				Weight:      550,
			}, {
				Destination: &networking.Destination{Host: "foo.baz.east"},
				Weight:      500,
			}},
		}, valid: true},
		{name: "total weight < 100", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz.south"},
				Weight:      49,
			}, {
				Destination: &networking.Destination{Host: "foo.baz.east"},
				Weight:      50,
			}},
		}, valid: true},
		{name: "simple redirect", route: &networking.HTTPRoute{
			Redirect: &networking.HTTPRedirect{
				Uri:       "/lerp",
				Authority: "foo.biz",
			},
		}, valid: true},
		{name: "conflicting redirect and route", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
			}},
			Redirect: &networking.HTTPRedirect{
				Uri:       "/lerp",
				Authority: "foo.biz",
			},
		}, valid: false},
		{name: "request response headers", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
			}},
		}, valid: true},
		{name: "valid headers", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Request: &networking.Headers_HeaderOperations{
						Add: map[string]string{
							"name": "",
						},
						Set: map[string]string{
							"name": "",
						},
						Remove: []string{
							"name",
						},
					},
					Response: &networking.Headers_HeaderOperations{
						Add: map[string]string{
							"name": "",
						},
						Set: map[string]string{
							"name": "",
						},
						Remove: []string{
							"name",
						},
					},
				},
			}},
		}, valid: true},
		{name: "empty header name - request add", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Request: &networking.Headers_HeaderOperations{
						Add: map[string]string{
							"": "value",
						},
					},
				},
			}},
		}, valid: false},
		{name: "empty header name - request set", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Request: &networking.Headers_HeaderOperations{
						Set: map[string]string{
							"": "value",
						},
					},
				},
			}},
		}, valid: false},
		{name: "empty header name - request remove", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Request: &networking.Headers_HeaderOperations{
						Remove: []string{
							"",
						},
					},
				},
			}},
		}, valid: false},
		{name: "empty header name - response add", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Response: &networking.Headers_HeaderOperations{
						Add: map[string]string{
							"": "value",
						},
					},
				},
			}},
		}, valid: false},
		{name: "empty header name - response set", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Response: &networking.Headers_HeaderOperations{
						Set: map[string]string{
							"": "value",
						},
					},
				},
			}},
		}, valid: false},
		{name: "empty header name - response remove", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Response: &networking.Headers_HeaderOperations{
						Remove: []string{
							"",
						},
					},
				},
			}},
		}, valid: false},
		{name: "envoy escaped % set", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Response: &networking.Headers_HeaderOperations{
						Set: map[string]string{
							"i-love-istio": "100%%",
						},
					},
				},
			}},
		}, valid: true},
		{name: "envoy variable set", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Response: &networking.Headers_HeaderOperations{
						Set: map[string]string{
							"name": "%HOSTNAME%",
						},
					},
				},
			}},
		}, valid: true},
		{name: "envoy unescaped % set", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Response: &networking.Headers_HeaderOperations{
						Set: map[string]string{
							"name": "abcd%oijasodifj",
						},
					},
				},
			}},
		}, valid: false},
		{name: "envoy escaped % add", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Response: &networking.Headers_HeaderOperations{
						Add: map[string]string{
							"i-love-istio": "100%% and more",
						},
					},
				},
			}},
		}, valid: true},
		{name: "envoy variable add", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Response: &networking.Headers_HeaderOperations{
						Add: map[string]string{
							"name": "hello %HOSTNAME%",
						},
					},
				},
			}},
		}, valid: true},
		{name: "envoy unescaped % add", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Response: &networking.Headers_HeaderOperations{
						Add: map[string]string{
							"name": "abcd%oijasodifj",
						},
					},
				},
			}},
		}, valid: false},
		{name: "null header match", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{{
				Headers: map[string]*networking.StringMatch{
					"header": nil,
				},
			}},
		}, valid: false},
		{name: "empty prefix header match", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{{
				Headers: map[string]*networking.StringMatch{
					"emptyprefix": {MatchType: &networking.StringMatch_Prefix{Prefix: ""}},
				},
			}},
		}, valid: false},
		{name: "nil match", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: nil,
		}, valid: true},
		{name: "match with nil element", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{nil},
		}, valid: true},
		{name: "invalid mirror percent", route: &networking.HTTPRoute{
			MirrorPercent: &wrapperspb.UInt32Value{Value: 101},
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{nil},
		}, valid: false},
		{name: "invalid mirror percentage", route: &networking.HTTPRoute{
			MirrorPercentage: &networking.Percent{
				Value: 101,
			},
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{nil},
		}, valid: false},
		{name: "valid mirror percentage", route: &networking.HTTPRoute{
			MirrorPercentage: &networking.Percent{
				Value: 1,
			},
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{nil},
		}, valid: true},
		{name: "negative mirror percentage", route: &networking.HTTPRoute{
			MirrorPercentage: &networking.Percent{
				Value: -1,
			},
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{nil},
		}, valid: false},
		{name: "mirrors without destination", route: &networking.HTTPRoute{
			Mirrors: []*networking.HTTPMirrorPolicy{{
				Destination: nil,
			}},
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{nil},
		}, valid: false},
		{name: "mirrors invalid mirror percentage", route: &networking.HTTPRoute{
			Mirrors: []*networking.HTTPMirrorPolicy{{
				Destination: &networking.Destination{Host: "foo.baz"},
			}, {
				Destination: &networking.Destination{Host: "foo.baz"},
				Percentage:  &networking.Percent{Value: 101},
			}},
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{nil},
		}, valid: false},
		{name: "mirrors valid mirror percentage", route: &networking.HTTPRoute{
			Mirrors: []*networking.HTTPMirrorPolicy{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Percentage:  &networking.Percent{Value: 1},
			}, {
				Destination: &networking.Destination{Host: "foo.baz"},
				Percentage:  &networking.Percent{Value: 50},
			}},
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{nil},
		}, valid: true},
		{name: "mirrors negative mirror percentage", route: &networking.HTTPRoute{
			Mirrors: []*networking.HTTPMirrorPolicy{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Percentage:  &networking.Percent{Value: -1},
			}},
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{nil},
		}, valid: false},
		{name: "conflicting mirror and mirrors", route: &networking.HTTPRoute{
			Mirror: &networking.Destination{Host: "foo.baz"},
			Mirrors: []*networking.HTTPMirrorPolicy{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
			}},
		}, valid: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateHTTPRoute(tc.route, false, false); (err.Err == nil) != tc.valid {
				t.Fatalf("got valid=%v but wanted valid=%v: %v", err.Err == nil, tc.valid, err)
			}
		})
	}
}

func TestValidateRouteDestination(t *testing.T) {
	testCases := []struct {
		name   string
		routes []*networking.RouteDestination
		valid  bool
	}{
		{name: "simple", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "foo.baz"},
		}}, valid: true},
		{name: "wildcard dash", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "*-foo.baz"},
		}}, valid: true},
		{name: "wildcard prefix", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "*foo.baz"},
		}}, valid: true},
		{name: "wildcard", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "*"},
		}}, valid: false},
		{name: "bad wildcard", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "foo.*"},
		}}, valid: false},
		{name: "bad fqdn", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "default/baz"},
		}}, valid: false},
		{name: "no destination", routes: []*networking.RouteDestination{{
			Destination: nil,
		}}, valid: false},
		{name: "weighted", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "foo.baz.south"},
			Weight:      25,
		}, {
			Destination: &networking.Destination{Host: "foo.baz.east"},
			Weight:      75,
		}}, valid: true},
		{name: "weight < 0", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "foo.baz.south"},
			Weight:      5,
		}, {
			Destination: &networking.Destination{Host: "foo.baz.east"},
			Weight:      -1,
		}}, valid: false},
		{name: "total weight > 100", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "foo.baz.south"},
			Weight:      550,
		}, {
			Destination: &networking.Destination{Host: "foo.baz.east"},
			Weight:      500,
		}}, valid: true},
		{name: "total weight < 100", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "foo.baz.south"},
			Weight:      49,
		}, {
			Destination: &networking.Destination{Host: "foo.baz.east"},
			Weight:      50,
		}}, valid: true},
		{name: "total weight = 100", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "foo.baz.south"},
			Weight:      100,
		}, {
			Destination: &networking.Destination{Host: "foo.baz.east"},
			Weight:      0,
		}}, valid: true},
		{name: "weight = 0", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "foo.baz.south"},
			Weight:      0,
		}}, valid: true},
		{name: "total weight = 0 with multi RouteDestination", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "foo.baz.south"},
			Weight:      0,
		}, {
			Destination: &networking.Destination{Host: "foo.baz.east"},
			Weight:      0,
		}}, valid: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateRouteDestinations(tc.routes, false); (err == nil) != tc.valid {
				t.Fatalf("got valid=%v but wanted valid=%v: %v", err == nil, tc.valid, err)
			}
		})
	}
}

// TODO: add TCP test cases once it is implemented
func TestValidateVirtualService(t *testing.T) {
	testCases := []struct {
		name    string
		in      proto.Message
		valid   bool
		warning bool
	}{
		{name: "simple", in: &networking.VirtualService{
			Hosts: []string{"foo.bar"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: true},
		{name: "allow sni based domains", in: &networking.VirtualService{
			Hosts:    []string{"outbound_.15010_._.istiod.istio-system.svc.cluster.local"},
			Gateways: []string{"ns1/gateway"},
			Tls: []*networking.TLSRoute{
				{
					Match: []*networking.TLSMatchAttributes{
						{
							SniHosts: []string{"outbound_.15010_._.istiod.istio-system.svc.cluster.local"},
						},
					},
					Route: []*networking.RouteDestination{
						{
							Destination: &networking.Destination{
								Host: "istio.istio-system.svc.cluster.local",
							},
						},
					},
				},
			},
		}, valid: true},
		{name: "duplicate hosts", in: &networking.VirtualService{
			Hosts: []string{"*.foo.bar", "*.bar"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: false},
		{name: "with no destination", in: &networking.VirtualService{
			Hosts: []string{"*.foo.bar", "*.bar"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{}},
			}},
		}, valid: false},
		{name: "destination with out hosts", in: &networking.VirtualService{
			Hosts: []string{"*.foo.bar", "*.bar"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{},
				}},
			}},
		}, valid: false},
		{name: "delegate with no hosts", in: &networking.VirtualService{
			Hosts: nil,
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: true},
		{name: "bad host", in: &networking.VirtualService{
			Hosts: []string{"foo.ba!r"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: false},
		{name: "no tcp or http routing", in: &networking.VirtualService{
			Hosts: []string{"foo.bar"},
		}, valid: false},
		{name: "bad gateway", in: &networking.VirtualService{
			Hosts:    []string{"foo.bar"},
			Gateways: []string{"b@dgateway"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: false},
		{name: "FQDN for gateway", in: &networking.VirtualService{
			Hosts:    []string{"foo.bar"},
			Gateways: []string{"gateway.example.com"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: true, warning: true},
		{name: "namespace/name for gateway", in: &networking.VirtualService{
			Hosts:    []string{"foo.bar"},
			Gateways: []string{"ns1/gateway"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: true},
		{name: "namespace/* for gateway", in: &networking.VirtualService{
			Hosts:    []string{"foo.bar"},
			Gateways: []string{"ns1/*"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: false},
		{name: "*/name for gateway", in: &networking.VirtualService{
			Hosts:    []string{"foo.bar"},
			Gateways: []string{"*/gateway"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: false},
		{name: "wildcard for mesh gateway", in: &networking.VirtualService{
			Hosts: []string{"*"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: false},
		{name: "wildcard for non-mesh gateway", in: &networking.VirtualService{
			Hosts:    []string{"*"},
			Gateways: []string{"somegateway"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: true},
		{name: "missing tcp route", in: &networking.VirtualService{
			Hosts: []string{"foo.bar"},
			Tcp: []*networking.TCPRoute{{
				Match: []*networking.L4MatchAttributes{
					{Port: 999},
				},
			}},
		}, valid: false},
		{name: "missing tls route", in: &networking.VirtualService{
			Hosts: []string{"foo.bar"},
			Tls: []*networking.TLSRoute{{
				Match: []*networking.TLSMatchAttributes{
					{
						Port:     999,
						SniHosts: []string{"foo.bar"},
					},
				},
			}},
		}, valid: false},
		{name: "deprecated mirror", in: &networking.VirtualService{
			Hosts:    []string{"foo.bar"},
			Gateways: []string{"ns1/gateway"},
			Http: []*networking.HTTPRoute{{
				MirrorPercent: &wrapperspb.UInt32Value{Value: 5},
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: true, warning: true},
		{name: "set authority", in: &networking.VirtualService{
			Hosts: []string{"foo.bar"},
			Http: []*networking.HTTPRoute{{
				Headers: &networking.Headers{
					Request: &networking.Headers_HeaderOperations{Set: map[string]string{":authority": "foo"}},
				},
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: true, warning: false},
		{name: "set authority in destination", in: &networking.VirtualService{
			Hosts: []string{"foo.bar"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
					Headers: &networking.Headers{
						Request: &networking.Headers_HeaderOperations{Set: map[string]string{":authority": "foo"}},
					},
				}},
			}},
		}, valid: true, warning: false},
		{name: "set authority in rewrite and header", in: &networking.VirtualService{
			Hosts: []string{"foo.bar"},
			Http: []*networking.HTTPRoute{{
				Headers: &networking.Headers{
					Request: &networking.Headers_HeaderOperations{Set: map[string]string{":authority": "foo"}},
				},
				Rewrite: &networking.HTTPRewrite{Authority: "bar"},
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: false, warning: false},
		{name: "non-method-get", in: &networking.VirtualService{
			Hosts: []string{"foo.bar"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
				Match: []*networking.HTTPMatchRequest{
					{
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Prefix{Prefix: "/api/v1/product"},
						},
					},
					{
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Prefix{Prefix: "/api/v1/products"},
						},
						Method: &networking.StringMatch{
							MatchType: &networking.StringMatch_Exact{Exact: "GET"},
						},
					},
				},
			}},
		}, valid: true, warning: true},
		{name: "uri-with-prefix-exact", in: &networking.VirtualService{
			Hosts: []string{"foo.bar"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
				Match: []*networking.HTTPMatchRequest{
					{
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Prefix{Prefix: "/"},
						},
					},
					{
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Exact{Exact: "/"},
						},
						Method: &networking.StringMatch{
							MatchType: &networking.StringMatch_Exact{Exact: "GET"},
						},
					},
				},
			}},
		}, valid: true, warning: false},
		{name: "jwt claim route without gateway", in: &networking.VirtualService{
			Hosts:    []string{"foo.bar"},
			Gateways: []string{"mesh"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
				Match: []*networking.HTTPMatchRequest{
					{
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Prefix{Prefix: "/"},
						},
						Headers: map[string]*networking.StringMatch{
							"@request.auth.claims.foo": {
								MatchType: &networking.StringMatch_Exact{Exact: "bar"},
							},
						},
					},
				},
			}},
		}, valid: false, warning: false},
		{name: "ip address as sni host", in: &networking.VirtualService{
			Hosts: []string{"foo.bar"},
			Tls: []*networking.TLSRoute{{
				Route: []*networking.RouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
				Match: []*networking.TLSMatchAttributes{
					{
						Port:     999,
						SniHosts: []string{"1.1.1.1"},
					},
				},
			}},
		}, valid: true, warning: true},
		{name: "invalid wildcard as sni host", in: &networking.VirtualService{
			Hosts: []string{"foo.bar"},
			Tls: []*networking.TLSRoute{{
				Route: []*networking.RouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
				Match: []*networking.TLSMatchAttributes{
					{
						Port:     999,
						SniHosts: []string{"foo.*.com"},
					},
				},
			}},
		}, valid: false, warning: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			warn, err := ValidateVirtualService(config.Config{Spec: tc.in})
			checkValidation(t, warn, err, tc.valid, tc.warning)
		})
	}
}

func TestValidateWorkloadEntry(t *testing.T) {
	testCases := []struct {
		name    string
		in      proto.Message
		valid   bool
		warning bool
	}{
		{
			name:  "valid",
			in:    &networking.WorkloadEntry{Address: "1.2.3.4"},
			valid: true,
		},
		{
			name:  "missing address",
			in:    &networking.WorkloadEntry{},
			valid: false,
		},
		{
			name:    "missing address with network",
			in:      &networking.WorkloadEntry{Network: "network-2"},
			valid:   true,
			warning: true,
		},
		{
			name:  "valid unix endpoint",
			in:    &networking.WorkloadEntry{Address: "unix:///lon/google/com"},
			valid: true,
		},
		{
			name:  "invalid unix endpoint",
			in:    &networking.WorkloadEntry{Address: "unix:///lon/google/com", Ports: map[string]uint32{"7777": 7777}},
			valid: false,
		},
		{
			name:  "valid FQDN",
			in:    &networking.WorkloadEntry{Address: "validdns.com", Ports: map[string]uint32{"7777": 7777}},
			valid: true,
		},
		{
			name:  "invalid FQDN",
			in:    &networking.WorkloadEntry{Address: "invaliddns.com:9443", Ports: map[string]uint32{"7777": 7777}},
			valid: false,
		},
		{
			name:  "valid IP",
			in:    &networking.WorkloadEntry{Address: "172.16.1.1", Ports: map[string]uint32{"7777": 7777}},
			valid: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			warn, err := ValidateWorkloadEntry(config.Config{Spec: tc.in})
			checkValidation(t, warn, err, tc.valid, tc.warning)
		})
	}
}

func TestValidateWorkloadGroup(t *testing.T) {
	testCases := []struct {
		name    string
		in      proto.Message
		valid   bool
		warning bool
	}{
		{
			name:  "valid",
			in:    &networking.WorkloadGroup{Template: &networking.WorkloadEntry{}},
			valid: true,
		},
		{
			name: "invalid",
			in: &networking.WorkloadGroup{Template: &networking.WorkloadEntry{}, Metadata: &networking.WorkloadGroup_ObjectMeta{Labels: map[string]string{
				".": "~",
			}}},
			valid: false,
		},
		{
			name: "probe missing method",
			in: &networking.WorkloadGroup{
				Template: &networking.WorkloadEntry{},
				Probe:    &networking.ReadinessProbe{},
			},
			valid: false,
		},
		{
			name: "probe nil",
			in: &networking.WorkloadGroup{
				Template: &networking.WorkloadEntry{},
				Probe: &networking.ReadinessProbe{
					HealthCheckMethod: &networking.ReadinessProbe_HttpGet{},
				},
			},
			valid: false,
		},
		{
			name: "probe http empty",
			in: &networking.WorkloadGroup{
				Template: &networking.WorkloadEntry{},
				Probe: &networking.ReadinessProbe{
					HealthCheckMethod: &networking.ReadinessProbe_HttpGet{
						HttpGet: &networking.HTTPHealthCheckConfig{},
					},
				},
			},
			valid: false,
		},
		{
			name: "probe http valid",
			in: &networking.WorkloadGroup{
				Template: &networking.WorkloadEntry{},
				Probe: &networking.ReadinessProbe{
					HealthCheckMethod: &networking.ReadinessProbe_HttpGet{
						HttpGet: &networking.HTTPHealthCheckConfig{
							Port: 5,
						},
					},
				},
			},
			valid: true,
		},
		{
			name: "probe tcp invalid",
			in: &networking.WorkloadGroup{
				Template: &networking.WorkloadEntry{},
				Probe: &networking.ReadinessProbe{
					HealthCheckMethod: &networking.ReadinessProbe_TcpSocket{
						TcpSocket: &networking.TCPHealthCheckConfig{},
					},
				},
			},
			valid: false,
		},
		{
			name: "probe tcp valid",
			in: &networking.WorkloadGroup{
				Template: &networking.WorkloadEntry{},
				Probe: &networking.ReadinessProbe{
					HealthCheckMethod: &networking.ReadinessProbe_TcpSocket{
						TcpSocket: &networking.TCPHealthCheckConfig{
							Port: 5,
						},
					},
				},
			},
			valid: true,
		},
		{
			name: "probe exec invalid",
			in: &networking.WorkloadGroup{
				Template: &networking.WorkloadEntry{},
				Probe: &networking.ReadinessProbe{
					HealthCheckMethod: &networking.ReadinessProbe_Exec{
						Exec: &networking.ExecHealthCheckConfig{},
					},
				},
			},
			valid: false,
		},
		{
			name: "probe exec valid",
			in: &networking.WorkloadGroup{
				Template: &networking.WorkloadEntry{},
				Probe: &networking.ReadinessProbe{
					HealthCheckMethod: &networking.ReadinessProbe_Exec{
						Exec: &networking.ExecHealthCheckConfig{
							Command: []string{"foo", "bar"},
						},
					},
				},
			},
			valid: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			warn, err := ValidateWorkloadGroup(config.Config{Spec: tc.in})
			checkValidation(t, warn, err, tc.valid, tc.warning)
		})
	}
}

func checkValidation(t *testing.T, gotWarning Warning, gotError error, valid bool, warning bool) {
	t.Helper()
	if (gotError == nil) != valid {
		t.Fatalf("got valid=%v but wanted valid=%v: %v", gotError == nil, valid, gotError)
	}
	if (gotWarning == nil) == warning {
		t.Fatalf("got warning=%v but wanted warning=%v", gotWarning, warning)
	}
}

func stringOrEmpty(v error) string {
	if v == nil {
		return ""
	}
	return v.Error()
}

func checkValidationMessage(t *testing.T, gotWarning Warning, gotError error, wantWarning string, wantError string) {
	t.Helper()
	if (gotError == nil) != (wantError == "") {
		t.Fatalf("got err=%v but wanted err=%v", gotError, wantError)
	}
	if !strings.Contains(stringOrEmpty(gotError), wantError) {
		t.Fatalf("got err=%v but wanted err=%v", gotError, wantError)
	}

	if (gotWarning == nil) != (wantWarning == "") {
		t.Fatalf("got warning=%v but wanted warning=%v", gotWarning, wantWarning)
	}
	if !strings.Contains(stringOrEmpty(gotWarning), wantWarning) {
		t.Fatalf("got warning=%v but wanted warning=%v", gotWarning, wantWarning)
	}
}

func TestValidateDestinationRule(t *testing.T) {
	cases := []struct {
		name    string
		in      proto.Message
		valid   bool
		warning bool
	}{
		{name: "simple destination rule", in: &networking.DestinationRule{
			Host: "reviews",
			Subsets: []*networking.Subset{
				{Name: "v1", Labels: map[string]string{"version": "v1"}},
				{Name: "v2", Labels: map[string]string{"version": "v2"}},
			},
		}, valid: true},

		{name: "simple destination rule with empty selector labels", in: &networking.DestinationRule{
			Host: "reviews",
			Subsets: []*networking.Subset{
				{Name: "v1", Labels: map[string]string{"version": "v1"}},
				{Name: "v2", Labels: map[string]string{"version": "v2"}},
			},
			WorkloadSelector: &api.WorkloadSelector{},
		}, valid: true, warning: true},

		{name: "missing destination name", in: &networking.DestinationRule{
			Host: "",
			Subsets: []*networking.Subset{
				{Name: "v1", Labels: map[string]string{"version": "v1"}},
				{Name: "v2", Labels: map[string]string{"version": "v2"}},
			},
		}, valid: false},

		{name: "missing subset name", in: &networking.DestinationRule{
			Host: "reviews",
			Subsets: []*networking.Subset{
				{Name: "", Labels: map[string]string{"version": "v1"}},
				{Name: "v2", Labels: map[string]string{"version": "v2"}},
			},
		}, valid: false},

		{name: "duplicate subset names", in: &networking.DestinationRule{
			Host: "reviews",
			Subsets: []*networking.Subset{
				{Name: "foo", Labels: map[string]string{"version": "v1"}},
				{Name: "foo", Labels: map[string]string{"version": "v2"}},
			},
		}, valid: false},

		{name: "valid traffic policy, top level", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				ConnectionPool: &networking.ConnectionPoolSettings{
					Tcp:  &networking.ConnectionPoolSettings_TCPSettings{MaxConnections: 7},
					Http: &networking.ConnectionPoolSettings_HTTPSettings{Http2MaxRequests: 11},
				},
				OutlierDetection: &networking.OutlierDetection{
					MinHealthPercent: 20,
				},
			},
			Subsets: []*networking.Subset{
				{Name: "v1", Labels: map[string]string{"version": "v1"}},
				{Name: "v2", Labels: map[string]string{"version": "v2"}},
			},
		}, valid: true},

		{name: "invalid traffic policy, top level", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				ConnectionPool: &networking.ConnectionPoolSettings{},
				OutlierDetection: &networking.OutlierDetection{
					MinHealthPercent: 20,
				},
			},
			Subsets: []*networking.Subset{
				{Name: "v1", Labels: map[string]string{"version": "v1"}},
				{Name: "v2", Labels: map[string]string{"version": "v2"}},
			},
		}, valid: false},

		{name: "valid traffic policy, subset level", in: &networking.DestinationRule{
			Host: "reviews",
			Subsets: []*networking.Subset{
				{
					Name: "v1", Labels: map[string]string{"version": "v1"},
					TrafficPolicy: &networking.TrafficPolicy{
						LoadBalancer: &networking.LoadBalancerSettings{
							LbPolicy: &networking.LoadBalancerSettings_Simple{
								Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
							},
						},
						ConnectionPool: &networking.ConnectionPoolSettings{
							Tcp:  &networking.ConnectionPoolSettings_TCPSettings{MaxConnections: 7},
							Http: &networking.ConnectionPoolSettings_HTTPSettings{Http2MaxRequests: 11},
						},
						OutlierDetection: &networking.OutlierDetection{
							MinHealthPercent: 20,
						},
					},
				},
				{Name: "v2", Labels: map[string]string{"version": "v2"}},
			},
		}, valid: true},

		{name: "invalid traffic policy, subset level", in: &networking.DestinationRule{
			Host: "reviews",
			Subsets: []*networking.Subset{
				{
					Name: "v1", Labels: map[string]string{"version": "v1"},
					TrafficPolicy: &networking.TrafficPolicy{
						LoadBalancer: &networking.LoadBalancerSettings{
							LbPolicy: &networking.LoadBalancerSettings_Simple{
								Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
							},
						},
						ConnectionPool: &networking.ConnectionPoolSettings{},
						OutlierDetection: &networking.OutlierDetection{
							MinHealthPercent: 20,
						},
					},
				},
				{Name: "v2", Labels: map[string]string{"version": "v2"}},
			},
		}, valid: false},

		{name: "valid traffic policy, both levels", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				ConnectionPool: &networking.ConnectionPoolSettings{
					Tcp:  &networking.ConnectionPoolSettings_TCPSettings{MaxConnections: 7},
					Http: &networking.ConnectionPoolSettings_HTTPSettings{Http2MaxRequests: 11},
				},
				OutlierDetection: &networking.OutlierDetection{
					MinHealthPercent: 20,
				},
			},
			Subsets: []*networking.Subset{
				{
					Name: "v1", Labels: map[string]string{"version": "v1"},
					TrafficPolicy: &networking.TrafficPolicy{
						LoadBalancer: &networking.LoadBalancerSettings{
							LbPolicy: &networking.LoadBalancerSettings_Simple{
								Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
							},
						},
						ConnectionPool: &networking.ConnectionPoolSettings{
							Tcp:  &networking.ConnectionPoolSettings_TCPSettings{MaxConnections: 7},
							Http: &networking.ConnectionPoolSettings_HTTPSettings{Http2MaxRequests: 11},
						},
						OutlierDetection: &networking.OutlierDetection{
							MinHealthPercent: 30,
						},
					},
				},
				{Name: "v2", Labels: map[string]string{"version": "v2"}},
			},
		}, valid: true},

		{name: "InsecureSkipVerify is not specified with tls mode simple, and the ca cert is specified by CaCertificates", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode:            networking.ClientTLSSettings_SIMPLE,
					CaCertificates:  "test",
					SubjectAltNames: []string{"reviews.default.svc"},
				},
			},
		}, valid: true},

		{name: "InsecureSkipVerify is not specified with tls mode simple, and the ca cert is specified by CredentialName", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode:            networking.ClientTLSSettings_SIMPLE,
					CredentialName:  "test",
					SubjectAltNames: []string{"reviews.default.svc"},
				},
			},
		}, valid: true},

		{name: "InsecureSkipVerify is set false with tls mode simple, and the ca cert is specified by CaCertificates", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode:            networking.ClientTLSSettings_SIMPLE,
					CaCertificates:  "test",
					SubjectAltNames: []string{"reviews.default.svc"},
					InsecureSkipVerify: &wrapperspb.BoolValue{
						Value: false,
					},
				},
			},
		}, valid: true},

		{name: "InsecureSkipVerify is set false with tls mode simple, and the ca cert is specified by CredentialName", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode:            networking.ClientTLSSettings_SIMPLE,
					CredentialName:  "test",
					SubjectAltNames: []string{"reviews.default.svc"},
					InsecureSkipVerify: &wrapperspb.BoolValue{
						Value: false,
					},
				},
			},
		}, valid: true},

		{name: "InsecureSkipVerify is set true with tls mode simple, and the ca cert is specified by CaCertificates", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode:           networking.ClientTLSSettings_SIMPLE,
					CredentialName: "test",
					InsecureSkipVerify: &wrapperspb.BoolValue{
						Value: true,
					},
				},
			},
		}, valid: false},

		{name: "InsecureSkipVerify is set true with tls mode simple, and the ca cert is specified by CredentialName", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode:           networking.ClientTLSSettings_SIMPLE,
					CaCertificates: "test",
					InsecureSkipVerify: &wrapperspb.BoolValue{
						Value: true,
					},
				},
			},
		}, valid: false},

		{name: "InsecureSkipVerify is set true with tls mode simple, and the san is specified", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode:            networking.ClientTLSSettings_SIMPLE,
					SubjectAltNames: []string{"reviews.default.svc"},
					InsecureSkipVerify: &wrapperspb.BoolValue{
						Value: true,
					},
				},
			},
		}, valid: false},

		{name: "InsecureSkipVerify is not specified with tls mode mutual, and the ca cert is specified by CaCertificates", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode:              networking.ClientTLSSettings_MUTUAL,
					CaCertificates:    "test",
					PrivateKey:        "key",
					ClientCertificate: "cert",
					SubjectAltNames:   []string{"reviews.default.svc"},
				},
			},
		}, valid: true},

		{name: "InsecureSkipVerify is not specified with tls mode mutual, and the ca cert is specified by CredentialName", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode:            networking.ClientTLSSettings_MUTUAL,
					CredentialName:  "test",
					SubjectAltNames: []string{"reviews.default.svc"},
				},
			},
		}, valid: true},

		{name: "InsecureSkipVerify is set false with tls mode mutual, and the ca cert is specified by CaCertificates", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode:              networking.ClientTLSSettings_MUTUAL,
					CaCertificates:    "test",
					PrivateKey:        "key",
					ClientCertificate: "cert",
					SubjectAltNames:   []string{"reviews.default.svc"},
					InsecureSkipVerify: &wrapperspb.BoolValue{
						Value: false,
					},
				},
			},
		}, valid: true},

		{name: "InsecureSkipVerify is set false with tls mode mutual, and the ca cert is specified by CredentialName", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode:            networking.ClientTLSSettings_MUTUAL,
					CredentialName:  "test",
					SubjectAltNames: []string{"reviews.default.svc"},
					InsecureSkipVerify: &wrapperspb.BoolValue{
						Value: false,
					},
				},
			},
		}, valid: true},

		{name: "InsecureSkipVerify is set true with tls mode mutual, and the ca cert is specified by CaCertificates", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode:              networking.ClientTLSSettings_MUTUAL,
					CaCertificates:    "test",
					PrivateKey:        "key",
					ClientCertificate: "cert",
					InsecureSkipVerify: &wrapperspb.BoolValue{
						Value: true,
					},
				},
			},
		}, valid: false},

		{name: "InsecureSkipVerify is set true with tls mode mutual, and the ca cert is specified by CaCertificates", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode:           networking.ClientTLSSettings_MUTUAL,
					CredentialName: "test",
					InsecureSkipVerify: &wrapperspb.BoolValue{
						Value: true,
					},
				},
			},
		}, valid: true},

		{name: "InsecureSkipVerify is set true with tls mode mutual, and the ca cert is not specified", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode:              networking.ClientTLSSettings_MUTUAL,
					PrivateKey:        "key",
					ClientCertificate: "cert",
					InsecureSkipVerify: &wrapperspb.BoolValue{
						Value: true,
					},
				},
			},
		}, valid: true},

		{name: "InsecureSkipVerify is set true with tls mode mutual, and the san is specified", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode:              networking.ClientTLSSettings_MUTUAL,
					PrivateKey:        "key",
					ClientCertificate: "cert",
					SubjectAltNames:   []string{"reviews.default.svc"},
					InsecureSkipVerify: &wrapperspb.BoolValue{
						Value: true,
					},
				},
			},
		}, valid: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			warn, got := ValidateDestinationRule(config.Config{
				Meta: config.Meta{
					Name:      someName,
					Namespace: someNamespace,
				},
				Spec: c.in,
			})
			if (got == nil) != c.valid {
				t.Errorf("ValidateDestinationRule failed on %v: got valid=%v but wanted valid=%v: %v",
					c.name, got == nil, c.valid, got)
			}
			if (warn == nil) == c.warning {
				t.Errorf("ValidateDestinationRule failed on %v: got warn=%v but wanted warn=%v: %v",
					c.name, warn == nil, c.warning, warn)
			}
		})
	}
}

func TestValidateTrafficPolicy(t *testing.T) {
	cases := []struct {
		name  string
		in    *networking.TrafficPolicy
		valid bool
	}{
		{
			name: "valid traffic policy", in: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				ConnectionPool: &networking.ConnectionPoolSettings{
					Tcp:  &networking.ConnectionPoolSettings_TCPSettings{MaxConnections: 7},
					Http: &networking.ConnectionPoolSettings_HTTPSettings{Http2MaxRequests: 11},
				},
				OutlierDetection: &networking.OutlierDetection{
					MinHealthPercent: 20,
				},
			},
			valid: true,
		},
		{
			name: "invalid traffic policy, nil entries", in: &networking.TrafficPolicy{},
			valid: false,
		},

		{
			name: "invalid traffic policy, missing port in port level settings", in: &networking.TrafficPolicy{
				PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
					{
						LoadBalancer: &networking.LoadBalancerSettings{
							LbPolicy: &networking.LoadBalancerSettings_Simple{
								Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
							},
						},
						ConnectionPool: &networking.ConnectionPoolSettings{
							Tcp:  &networking.ConnectionPoolSettings_TCPSettings{MaxConnections: 7},
							Http: &networking.ConnectionPoolSettings_HTTPSettings{Http2MaxRequests: 11},
						},
						OutlierDetection: &networking.OutlierDetection{
							MinHealthPercent: 20,
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "invalid traffic policy, bad connection pool", in: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				ConnectionPool: &networking.ConnectionPoolSettings{},
				OutlierDetection: &networking.OutlierDetection{
					MinHealthPercent: 20,
				},
			},
			valid: false,
		},
		{
			name: "invalid traffic policy, bad max connection duration", in: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				ConnectionPool: &networking.ConnectionPoolSettings{
					Tcp: &networking.ConnectionPoolSettings_TCPSettings{
						MaxConnectionDuration: &durationpb.Duration{Nanos: 500},
					},
				},
			},
			valid: false,
		},
		{
			name: "invalid traffic policy, panic threshold too low", in: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				ConnectionPool: &networking.ConnectionPoolSettings{
					Tcp:  &networking.ConnectionPoolSettings_TCPSettings{MaxConnections: 7},
					Http: &networking.ConnectionPoolSettings_HTTPSettings{Http2MaxRequests: 11},
				},
				OutlierDetection: &networking.OutlierDetection{
					MinHealthPercent: -1,
				},
			},
			valid: false,
		},
		{
			name: "invalid traffic policy, both upgrade and use client protocol set", in: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						H2UpgradePolicy:   networking.ConnectionPoolSettings_HTTPSettings_UPGRADE,
						UseClientProtocol: true,
					},
				},
			},
			valid: false,
		},
	}
	for _, c := range cases {
		if got := validateTrafficPolicy("", c.in).Err; (got == nil) != c.valid {
			t.Errorf("ValidateTrafficPolicy failed on %v: got valid=%v but wanted valid=%v: %v",
				c.name, got == nil, c.valid, got)
		}
	}
}

func TestValidateConnectionPool(t *testing.T) {
	cases := []struct {
		name  string
		in    *networking.ConnectionPoolSettings
		valid bool
	}{
		{
			name: "valid connection pool, tcp and http", in: &networking.ConnectionPoolSettings{
				Tcp: &networking.ConnectionPoolSettings_TCPSettings{
					MaxConnections: 7,
					ConnectTimeout: &durationpb.Duration{Seconds: 2},
				},
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					Http1MaxPendingRequests:  2,
					Http2MaxRequests:         11,
					MaxRequestsPerConnection: 5,
					MaxRetries:               4,
					IdleTimeout:              &durationpb.Duration{Seconds: 30},
					MaxConcurrentStreams:     5,
				},
			},
			valid: true,
		},

		{
			name: "valid connection pool, tcp only", in: &networking.ConnectionPoolSettings{
				Tcp: &networking.ConnectionPoolSettings_TCPSettings{
					MaxConnections: 7,
					ConnectTimeout: &durationpb.Duration{Seconds: 2},
				},
			},
			valid: true,
		},

		{
			name: "valid connection pool, http only", in: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					Http1MaxPendingRequests:  2,
					Http2MaxRequests:         11,
					MaxRequestsPerConnection: 5,
					MaxRetries:               4,
					IdleTimeout:              &durationpb.Duration{Seconds: 30},
					MaxConcurrentStreams:     5,
				},
			},
			valid: true,
		},

		{
			name: "valid connection pool, http only with empty idle timeout", in: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					Http1MaxPendingRequests:  2,
					Http2MaxRequests:         11,
					MaxRequestsPerConnection: 5,
					MaxRetries:               4,
					MaxConcurrentStreams:     5,
				},
			},
			valid: true,
		},

		{name: "invalid connection pool, empty", in: &networking.ConnectionPoolSettings{}, valid: false},

		{
			name: "invalid connection pool, bad max connections", in: &networking.ConnectionPoolSettings{
				Tcp: &networking.ConnectionPoolSettings_TCPSettings{MaxConnections: -1},
			},
			valid: false,
		},

		{
			name: "invalid connection pool, bad connect timeout", in: &networking.ConnectionPoolSettings{
				Tcp: &networking.ConnectionPoolSettings_TCPSettings{
					ConnectTimeout: &durationpb.Duration{Seconds: 2, Nanos: 5},
				},
			},
			valid: false,
		},

		{
			name: "invalid connection pool, bad max pending requests", in: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{Http1MaxPendingRequests: -1},
			},
			valid: false,
		},

		{
			name: "invalid connection pool, bad max requests", in: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{Http2MaxRequests: -1},
			},
			valid: false,
		},

		{
			name: "invalid connection pool, bad max requests per connection", in: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{MaxRequestsPerConnection: -1},
			},
			valid: false,
		},

		{
			name: "invalid connection pool, bad max retries", in: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{MaxRetries: -1},
			},
			valid: false,
		},

		{
			name: "invalid connection pool, bad idle timeout", in: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{IdleTimeout: &durationpb.Duration{Seconds: 30, Nanos: 5}},
			},
			valid: false,
		},

		{
			name: "valid connection pool, tcp timeout disabled", in: &networking.ConnectionPoolSettings{
				Tcp: &networking.ConnectionPoolSettings_TCPSettings{IdleTimeout: &durationpb.Duration{Seconds: 0}},
			},
			valid: true,
		},

		{
			name: "invalid connection pool, bad max concurrent streams", in: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{MaxConcurrentStreams: -1},
			},
			valid: false,
		},
	}

	for _, c := range cases {
		if got := validateConnectionPool(c.in); (got == nil) != c.valid {
			t.Errorf("ValidateConnectionSettings failed on %v: got valid=%v but wanted valid=%v: %v",
				c.name, got == nil, c.valid, got)
		}
	}
}

func TestValidateLoadBalancer(t *testing.T) {
	duration := durationpb.Duration{Seconds: int64(time.Hour / time.Second)}
	cases := []struct {
		name  string
		in    *networking.LoadBalancerSettings
		valid bool
	}{
		{
			name: "valid load balancer with simple load balancing", in: &networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_Simple{
					Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
				},
			},
			valid: true,
		},

		{
			name: "valid load balancer with consistentHash load balancing", in: &networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
					ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
						MinimumRingSize: 1024,
						HashKey: &networking.LoadBalancerSettings_ConsistentHashLB_HttpCookie{
							HttpCookie: &networking.LoadBalancerSettings_ConsistentHashLB_HTTPCookie{
								Name: "test",
								Ttl:  &duration,
							},
						},
					},
				},
			},
			valid: true,
		},

		{
			name: "invalid load balancer with consistentHash load balancing, missing name", in: &networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
					ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
						MinimumRingSize: 1024,
						HashKey: &networking.LoadBalancerSettings_ConsistentHashLB_HttpCookie{
							HttpCookie: &networking.LoadBalancerSettings_ConsistentHashLB_HTTPCookie{
								Ttl: &duration,
							},
						},
					},
				},
			},
			valid: false,
		},

		{
			name: "invalid load balancer with consistentHash load balancing, maglev not prime", in: &networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
					ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
						HashAlgorithm: &networking.LoadBalancerSettings_ConsistentHashLB_Maglev{
							Maglev: &networking.LoadBalancerSettings_ConsistentHashLB_MagLev{TableSize: 1000},
						},
					},
				},
			},
			valid: false,
		},
	}

	for _, c := range cases {
		if got := validateLoadBalancer(c.in, nil); (got.Err == nil) != c.valid {
			t.Errorf("validateLoadBalancer failed on %v: got valid=%v but wanted valid=%v: %v",
				c.name, got.Err == nil, c.valid, got)
		}
	}
}

func TestValidateOutlierDetection(t *testing.T) {
	cases := []struct {
		name  string
		in    *networking.OutlierDetection
		valid bool
		warn  bool
	}{
		{name: "valid outlier detection", in: &networking.OutlierDetection{
			Interval:           &durationpb.Duration{Seconds: 2},
			BaseEjectionTime:   &durationpb.Duration{Seconds: 2},
			MaxEjectionPercent: 50,
		}, valid: true},

		{
			name: "invalid outlier detection, bad interval", in: &networking.OutlierDetection{
				Interval: &durationpb.Duration{Seconds: 2, Nanos: 5},
			},
			valid: false,
		},

		{
			name: "invalid outlier detection, bad base ejection time", in: &networking.OutlierDetection{
				BaseEjectionTime: &durationpb.Duration{Seconds: 2, Nanos: 5},
			},
			valid: false,
		},

		{
			name: "invalid outlier detection, bad max ejection percent", in: &networking.OutlierDetection{
				MaxEjectionPercent: 105,
			},
			valid: false,
		},
		{
			name: "invalid outlier detection, panic threshold too low", in: &networking.OutlierDetection{
				MinHealthPercent: -1,
			},
			valid: false,
		},
		{
			name: "invalid outlier detection, panic threshold too high", in: &networking.OutlierDetection{
				MinHealthPercent: 101,
			},
			valid: false,
		},
		{
			name: "deprecated outlier detection, ConsecutiveErrors", in: &networking.OutlierDetection{
				ConsecutiveErrors: 101,
			},
			valid: true,
			warn:  true,
		},
		{
			name: "consecutive local origin errors is set but split local origin errors is not set", in: &networking.OutlierDetection{
				ConsecutiveLocalOriginFailures: &wrapperspb.UInt32Value{Value: 10},
			},
			valid: false,
		},
	}

	for _, c := range cases {
		got := validateOutlierDetection(c.in)
		if (got.Err == nil) != c.valid {
			t.Errorf("ValidateOutlierDetection failed on %v: got valid=%v but wanted valid=%v: %v",
				c.name, got.Err == nil, c.valid, got.Err)
		}
		if (got.Warning == nil) == c.warn {
			t.Errorf("ValidateOutlierDetection failed on %v: got warn=%v but wanted warn=%v: %v",
				c.name, got.Warning == nil, c.warn, got.Warning)
		}
	}
}

func TestValidateServiceEntries(t *testing.T) {
	cases := []struct {
		name    string
		in      *networking.ServiceEntry
		valid   bool
		warning bool
	}{
		{
			name: "discovery type DNS", in: &networking.ServiceEntry{
				Hosts: []string{"*.google.com"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Protocol: "http", Name: "http-valid2"},
				},
				Endpoints: []*networking.WorkloadEntry{
					{Address: "lon.google.com", Ports: map[string]uint32{"http-valid1": 8080}},
					{Address: "in.google.com", Ports: map[string]uint32{"http-valid2": 9080}},
				},
				Resolution: networking.ServiceEntry_DNS,
			},
			valid: true,
		},
		{
			name: "discovery type DNS Round Robin", in: &networking.ServiceEntry{
				Hosts: []string{"*.istio.io"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Protocol: "http", Name: "http-valid2"},
				},
				Endpoints: []*networking.WorkloadEntry{
					{Address: "api-v1.istio.io", Ports: map[string]uint32{"http-valid1": 8080}},
				},
				Resolution: networking.ServiceEntry_DNS_ROUND_ROBIN,
			},
			valid: true,
		},
		{
			name: "discovery type DNS, label tlsMode: istio", in: &networking.ServiceEntry{
				Hosts: []string{"*.google.com"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Protocol: "http", Name: "http-valid2"},
				},
				Endpoints: []*networking.WorkloadEntry{
					{Address: "lon.google.com", Ports: map[string]uint32{"http-valid1": 8080}, Labels: map[string]string{"security.istio.io/tlsMode": "istio"}},
					{Address: "in.google.com", Ports: map[string]uint32{"http-valid2": 9080}, Labels: map[string]string{"security.istio.io/tlsMode": "istio"}},
				},
				Resolution: networking.ServiceEntry_DNS,
			},
			valid: true,
		},
		{
			name: "discovery type DNS, one host set with IP address and https port",
			in: &networking.ServiceEntry{
				Hosts:     []string{"httpbin.org"},
				Addresses: []string{"10.10.10.10"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Protocol: "http", Name: "http-valid2"},
					{Number: 443, Protocol: "https", Name: "https"},
				},
				Resolution: networking.ServiceEntry_DNS,
			},
			valid:   true,
			warning: false,
		},

		{
			name: "discovery type DNS, multi hosts set with IP address and https port",
			in: &networking.ServiceEntry{
				Hosts:     []string{"httpbin.org", "wikipedia.org"},
				Addresses: []string{"10.10.10.10"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Protocol: "http", Name: "http-valid2"},
					{Number: 443, Protocol: "https", Name: "https"},
				},
				Resolution: networking.ServiceEntry_DNS,
			},
			valid:   true,
			warning: true,
		},

		{
			name: "discovery type DNS, IP address set",
			in: &networking.ServiceEntry{
				Hosts:     []string{"*.google.com"},
				Addresses: []string{"10.10.10.10"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Protocol: "http", Name: "http-valid2"},
				},
				Endpoints: []*networking.WorkloadEntry{
					{Address: "lon.google.com", Ports: map[string]uint32{"http-valid1": 8080}},
					{Address: "in.google.com", Ports: map[string]uint32{"http-valid2": 9080}},
				},
				Resolution: networking.ServiceEntry_DNS,
			},
			valid:   true,
			warning: false,
		},

		{
			name: "discovery type DNS, IP in endpoints", in: &networking.ServiceEntry{
				Hosts: []string{"*.google.com"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Protocol: "http", Name: "http-valid2"},
				},
				Endpoints: []*networking.WorkloadEntry{
					{Address: "1.1.1.1", Ports: map[string]uint32{"http-valid1": 8080}},
					{Address: "in.google.com", Ports: map[string]uint32{"http-valid2": 9080}},
				},
				Resolution: networking.ServiceEntry_DNS,
			},
			valid: true,
		},

		{
			name: "empty hosts", in: &networking.ServiceEntry{
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
				},
				Endpoints: []*networking.WorkloadEntry{
					{Address: "in.google.com", Ports: map[string]uint32{"http-valid2": 9080}},
				},
				Resolution: networking.ServiceEntry_DNS,
			},
			valid: false,
		},
		{
			name: "bad hosts", in: &networking.ServiceEntry{
				Hosts: []string{"-"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
				},
				Endpoints: []*networking.WorkloadEntry{
					{Address: "in.google.com", Ports: map[string]uint32{"http-valid2": 9080}},
				},
				Resolution: networking.ServiceEntry_DNS,
			},
			valid: false,
		},
		{
			name: "full wildcard host", in: &networking.ServiceEntry{
				Hosts: []string{"foo.com", "*"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
				},
				Endpoints: []*networking.WorkloadEntry{
					{Address: "in.google.com", Ports: map[string]uint32{"http-valid2": 9080}},
				},
				Resolution: networking.ServiceEntry_DNS,
			},
			valid: false,
		},
		{
			name: "short name host", in: &networking.ServiceEntry{
				Hosts: []string{"foo", "bar.com"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
				},
				Endpoints: []*networking.WorkloadEntry{
					{Address: "in.google.com", Ports: map[string]uint32{"http-valid1": 9080}},
				},
				Resolution: networking.ServiceEntry_DNS,
			},
			valid: true,
		},
		{
			name: "undefined endpoint port", in: &networking.ServiceEntry{
				Hosts: []string{"google.com"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 80, Protocol: "http", Name: "http-valid2"},
				},
				Endpoints: []*networking.WorkloadEntry{
					{Address: "lon.google.com", Ports: map[string]uint32{"http-valid1": 8080}},
					{Address: "in.google.com", Ports: map[string]uint32{"http-dne": 9080}},
				},
				Resolution: networking.ServiceEntry_DNS,
			},
			valid: false,
		},

		{
			name: "discovery type DNS, non-FQDN endpoint", in: &networking.ServiceEntry{
				Hosts: []string{"*.google.com"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Protocol: "http", Name: "http-valid2"},
				},
				Endpoints: []*networking.WorkloadEntry{
					{Address: "*.lon.google.com", Ports: map[string]uint32{"http-valid1": 8080}},
					{Address: "in.google.com", Ports: map[string]uint32{"http-dne": 9080}},
				},
				Resolution: networking.ServiceEntry_DNS,
			},
			valid: false,
		},

		{
			name: "discovery type DNS, non-FQDN host", in: &networking.ServiceEntry{
				Hosts: []string{"*.google.com"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Protocol: "http", Name: "http-valid2"},
				},

				Resolution: networking.ServiceEntry_DNS,
			},
			valid: false,
		},

		{
			name: "discovery type DNS, no endpoints", in: &networking.ServiceEntry{
				Hosts: []string{"google.com"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Protocol: "http", Name: "http-valid2"},
				},

				Resolution: networking.ServiceEntry_DNS,
			},
			valid: true,
		},

		{
			name: "discovery type DNS, unix endpoint", in: &networking.ServiceEntry{
				Hosts: []string{"*.google.com"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
				},
				Endpoints: []*networking.WorkloadEntry{
					{Address: "unix:///lon/google/com"},
				},
				Resolution: networking.ServiceEntry_DNS,
			},
			valid: false,
		},

		{
			name: "discovery type none", in: &networking.ServiceEntry{
				Hosts: []string{"google.com"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Protocol: "http", Name: "http-valid2"},
				},
				Resolution: networking.ServiceEntry_NONE,
			},
			valid: true,
		},

		{
			name: "discovery type none, endpoints provided", in: &networking.ServiceEntry{
				Hosts: []string{"google.com"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Protocol: "http", Name: "http-valid2"},
				},
				Endpoints: []*networking.WorkloadEntry{
					{Address: "lon.google.com", Ports: map[string]uint32{"http-valid1": 8080}},
				},
				Resolution: networking.ServiceEntry_NONE,
			},
			valid: false,
		},

		{
			name: "discovery type none, cidr addresses", in: &networking.ServiceEntry{
				Hosts:     []string{"google.com"},
				Addresses: []string{"172.1.2.16/16"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Protocol: "http", Name: "http-valid2"},
				},
				Resolution: networking.ServiceEntry_NONE,
			},
			valid: true,
		},

		{
			name: "discovery type static, cidr addresses with endpoints", in: &networking.ServiceEntry{
				Hosts:     []string{"google.com"},
				Addresses: []string{"172.1.2.16/16"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Protocol: "http", Name: "http-valid2"},
				},
				Endpoints: []*networking.WorkloadEntry{
					{Address: "1.1.1.1", Ports: map[string]uint32{"http-valid1": 8080}},
					{Address: "2.2.2.2", Ports: map[string]uint32{"http-valid2": 9080}},
				},
				Resolution: networking.ServiceEntry_STATIC,
			},
			valid: true,
		},

		{
			name: "discovery type static", in: &networking.ServiceEntry{
				Hosts:     []string{"google.com"},
				Addresses: []string{"172.1.2.16"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Protocol: "http", Name: "http-valid2"},
				},
				Endpoints: []*networking.WorkloadEntry{
					{Address: "1.1.1.1", Ports: map[string]uint32{"http-valid1": 8080}},
					{Address: "2.2.2.2", Ports: map[string]uint32{"http-valid2": 9080}},
				},
				Resolution: networking.ServiceEntry_STATIC,
			},
			valid: true,
		},

		{
			name: "discovery type static, FQDN in endpoints", in: &networking.ServiceEntry{
				Hosts:     []string{"google.com"},
				Addresses: []string{"172.1.2.16"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Protocol: "http", Name: "http-valid2"},
				},
				Endpoints: []*networking.WorkloadEntry{
					{Address: "google.com", Ports: map[string]uint32{"http-valid1": 8080}},
					{Address: "2.2.2.2", Ports: map[string]uint32{"http-valid2": 9080}},
				},
				Resolution: networking.ServiceEntry_STATIC,
			},
			valid: false,
		},

		{
			name: "discovery type static, missing endpoints", in: &networking.ServiceEntry{
				Hosts:     []string{"google.com"},
				Addresses: []string{"172.1.2.16"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Protocol: "http", Name: "http-valid2"},
				},
				Resolution: networking.ServiceEntry_STATIC,
			},
			valid: true,
		},

		{
			name: "discovery type static, bad endpoint port name", in: &networking.ServiceEntry{
				Hosts:     []string{"google.com"},
				Addresses: []string{"172.1.2.16"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Protocol: "http", Name: "http-valid2"},
				},
				Endpoints: []*networking.WorkloadEntry{
					{Address: "1.1.1.1", Ports: map[string]uint32{"http-valid1": 8080}},
					{Address: "2.2.2.2", Ports: map[string]uint32{"http-dne": 9080}},
				},
				Resolution: networking.ServiceEntry_STATIC,
			},
			valid: false,
		},

		{
			name: "discovery type none, conflicting port names", in: &networking.ServiceEntry{
				Hosts: []string{"google.com"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-conflict"},
					{Number: 8080, Protocol: "http", Name: "http-conflict"},
				},
				Resolution: networking.ServiceEntry_NONE,
			},
			valid: false,
		},

		{
			name: "discovery type none, conflicting port numbers", in: &networking.ServiceEntry{
				Hosts: []string{"google.com"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-conflict1"},
					{Number: 80, Protocol: "http", Name: "http-conflict2"},
				},
				Resolution: networking.ServiceEntry_NONE,
			},
			valid: false,
		},

		{
			name: "unix socket", in: &networking.ServiceEntry{
				Hosts: []string{"uds.cluster.local"},
				Ports: []*networking.ServicePort{
					{Number: 6553, Protocol: "grpc", Name: "grpc-service1"},
				},
				Resolution: networking.ServiceEntry_STATIC,
				Endpoints: []*networking.WorkloadEntry{
					{Address: "unix:///path/to/socket"},
				},
			},
			valid: true,
		},

		{
			name: "unix socket, relative path", in: &networking.ServiceEntry{
				Hosts: []string{"uds.cluster.local"},
				Ports: []*networking.ServicePort{
					{Number: 6553, Protocol: "grpc", Name: "grpc-service1"},
				},
				Resolution: networking.ServiceEntry_STATIC,
				Endpoints: []*networking.WorkloadEntry{
					{Address: "unix://./relative/path.sock"},
				},
			},
			valid: false,
		},

		{
			name: "unix socket, endpoint ports", in: &networking.ServiceEntry{
				Hosts: []string{"uds.cluster.local"},
				Ports: []*networking.ServicePort{
					{Number: 6553, Protocol: "grpc", Name: "grpc-service1"},
				},
				Resolution: networking.ServiceEntry_STATIC,
				Endpoints: []*networking.WorkloadEntry{
					{Address: "unix:///path/to/socket", Ports: map[string]uint32{"grpc-service1": 6553}},
				},
			},
			valid: false,
		},
		{
			name: "unix socket, multiple service ports", in: &networking.ServiceEntry{
				Hosts: []string{"uds.cluster.local"},
				Ports: []*networking.ServicePort{
					{Number: 6553, Protocol: "grpc", Name: "grpc-service1"},
					{Number: 80, Protocol: "http", Name: "http-service2"},
				},
				Resolution: networking.ServiceEntry_STATIC,
				Endpoints: []*networking.WorkloadEntry{
					{Address: "unix:///path/to/socket"},
				},
			},
			valid: false,
		},
		{
			name: "empty protocol", in: &networking.ServiceEntry{
				Hosts:     []string{"google.com"},
				Addresses: []string{"172.1.2.16/16"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Name: "http-valid2"},
				},
				Resolution: networking.ServiceEntry_NONE,
			},
			valid: true,
		},
		{
			name: "selector", in: &networking.ServiceEntry{
				Hosts:            []string{"google.com"},
				WorkloadSelector: &networking.WorkloadSelector{Labels: map[string]string{"foo": "bar"}},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
				},
			},
			valid:   true,
			warning: true,
		},
		{
			name: "workload selector without labels",
			in: &networking.ServiceEntry{
				Hosts: []string{"google.com"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Protocol: "http", Name: "http-valid2"},
				},
				WorkloadSelector: &networking.WorkloadSelector{},
			},
			valid:   true,
			warning: true,
		},
		{
			name: "selector and endpoints", in: &networking.ServiceEntry{
				Hosts:            []string{"google.com"},
				WorkloadSelector: &networking.WorkloadSelector{Labels: map[string]string{"foo": "bar"}},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
				},
				Endpoints: []*networking.WorkloadEntry{
					{Address: "1.1.1.1"},
				},
			},
			valid:   false,
			warning: true,
		},
		{
			name: "selector and resolution NONE", in: &networking.ServiceEntry{
				Hosts:            []string{"google.com"},
				WorkloadSelector: &networking.WorkloadSelector{Labels: map[string]string{"foo": "bar"}},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
				},
				Resolution: networking.ServiceEntry_NONE,
			},
			valid:   true,
			warning: true,
		},
		{
			name: "selector and resolution DNS", in: &networking.ServiceEntry{
				Hosts:            []string{"google.com"},
				WorkloadSelector: &networking.WorkloadSelector{Labels: map[string]string{"foo": "bar"}},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
				},
				Resolution: networking.ServiceEntry_DNS,
			},
			valid:   true,
			warning: false,
		},
		{
			name: "bad selector key", in: &networking.ServiceEntry{
				Hosts:            []string{"google.com"},
				WorkloadSelector: &networking.WorkloadSelector{Labels: map[string]string{"": "bar"}},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
				},
			},
			valid: false,
		},
		{
			name: "repeat target port", in: &networking.ServiceEntry{
				Hosts:      []string{"google.com"},
				Resolution: networking.ServiceEntry_DNS,
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1", TargetPort: 80},
					{Number: 81, Protocol: "http", Name: "http-valid2", TargetPort: 80},
				},
			},
			valid: true,
		},
		{
			name: "valid target port", in: &networking.ServiceEntry{
				Hosts:      []string{"google.com"},
				Resolution: networking.ServiceEntry_DNS,
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1", TargetPort: 81},
				},
			},
			valid: true,
		},
		{
			name: "invalid target port", in: &networking.ServiceEntry{
				Hosts:      []string{"google.com"},
				Resolution: networking.ServiceEntry_DNS,
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1", TargetPort: 65536},
				},
			},
			valid: false,
		},
		{
			name: "warn target port", in: &networking.ServiceEntry{
				Hosts:            []string{"google.com"},
				WorkloadSelector: &networking.WorkloadSelector{Labels: map[string]string{"key": "bar"}},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1", TargetPort: 1234},
				},
			},
			valid:   true,
			warning: true,
		},
		{
			name: "valid endpoint port", in: &networking.ServiceEntry{
				Hosts: []string{"google.com"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1", TargetPort: 81},
				},
				Resolution: networking.ServiceEntry_STATIC,
				Endpoints: []*networking.WorkloadEntry{
					{
						Address: "1.1.1.1",
						Ports: map[string]uint32{
							"http-valid1": 8081,
						},
					},
				},
			},
			valid: true,
		},
		{
			name: "invalid endpoint port", in: &networking.ServiceEntry{
				Hosts: []string{"google.com"},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1", TargetPort: 81},
				},
				Resolution: networking.ServiceEntry_STATIC,
				Endpoints: []*networking.WorkloadEntry{
					{
						Address: "1.1.1.1",
						Ports: map[string]uint32{
							"http-valid1": 65536,
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "protocol unset for addresses empty", in: &networking.ServiceEntry{
				Hosts:     []string{"google.com"},
				Addresses: []string{},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 8080, Name: "http-valid2"},
				},
				Resolution: networking.ServiceEntry_NONE,
			},
			valid:   true,
			warning: true,
		},
		{
			name: "protocol is TCP for addresses empty", in: &networking.ServiceEntry{
				Hosts:     []string{"google.com"},
				Addresses: []string{},
				Ports: []*networking.ServicePort{
					{Number: 80, Protocol: "http", Name: "http-valid1"},
					{Number: 81, Protocol: "TCP", Name: "tcp-valid1"},
				},
				Resolution: networking.ServiceEntry_NONE,
			},
			valid:   true,
			warning: true,
		},
		{
			name: "dns round robin with more than one endpoint", in: &networking.ServiceEntry{
				Hosts:     []string{"google.com"},
				Addresses: []string{},
				Ports: []*networking.ServicePort{
					{Number: 8081, Protocol: "http", Name: "http-valid1"},
				},
				Endpoints: []*networking.WorkloadEntry{
					{
						Address: "api-v1.istio.io",
						Ports: map[string]uint32{
							"http-valid1": 8081,
						},
					},
					{
						Address: "1.1.1.2",
						Ports: map[string]uint32{
							"http-valid1": 8081,
						},
					},
				},
				Resolution: networking.ServiceEntry_DNS_ROUND_ROBIN,
			},
			valid:   false,
			warning: false,
		},
		{
			name: "dns round robin with 0 endpoints", in: &networking.ServiceEntry{
				Hosts:     []string{"google.com"},
				Addresses: []string{},
				Ports: []*networking.ServicePort{
					{Number: 8081, Protocol: "http", Name: "http-valid1"},
				},
				Endpoints:  []*networking.WorkloadEntry{},
				Resolution: networking.ServiceEntry_DNS_ROUND_ROBIN,
			},
			valid:   true,
			warning: false,
		},
		{
			name: "partial wildcard hosts", in: &networking.ServiceEntry{
				Hosts:     []string{"*.nytimes.com", "*washingtonpost.com"},
				Addresses: []string{},
				Ports: []*networking.ServicePort{
					{Number: 443, Protocol: "HTTPS", Name: "https-nytimes"},
				},
				Endpoints:  []*networking.WorkloadEntry{},
				Resolution: networking.ServiceEntry_NONE,
			},
			valid:   true,
			warning: true,
		},
		{
			name: "network set with no address on endpoint", in: &networking.ServiceEntry{
				Hosts:     []string{"www.example.com"},
				Addresses: []string{},
				Ports: []*networking.ServicePort{
					{Number: 443, Protocol: "HTTPS", Name: "https-example"},
				},
				Endpoints: []*networking.WorkloadEntry{
					{Network: "cluster-1"},
				},
				Resolution: networking.ServiceEntry_STATIC,
			},
			valid:   true,
			warning: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			warning, err := ValidateServiceEntry(config.Config{
				Meta: config.Meta{
					Name:      someName,
					Namespace: someNamespace,
				},
				Spec: c.in,
			})
			if (err == nil) != c.valid {
				t.Errorf("ValidateServiceEntry got valid=%v but wanted valid=%v: %v",
					err == nil, c.valid, err)
			}
			if (warning != nil) != c.warning {
				t.Errorf("ValidateServiceEntry got warning=%v but wanted warning=%v: %v",
					warning != nil, c.warning, warning)
			}
		})
	}
}

func TestValidateAuthorizationPolicy(t *testing.T) {
	cases := []struct {
		name        string
		annotations map[string]string
		in          proto.Message
		valid       bool
		Warning     bool
	}{
		{
			name: "good",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app":     "httpbin",
						"version": "v1",
					},
				},
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									Principals: []string{"sa1"},
								},
							},
							{
								Source: &security_beta.Source{
									Principals: []string{"sa2"},
								},
							},
						},
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Methods: []string{"GET"},
								},
							},
							{
								Operation: &security_beta.Operation{
									Methods: []string{"POST"},
								},
							},
						},
						When: []*security_beta.Condition{
							{
								Key:    "source.ip",
								Values: []string{"1.2.3.4", "5.6.7.0/24"},
							},
							{
								Key:    "request.headers[:authority]",
								Values: []string{"v1", "v2"},
							},
						},
					},
				},
			},
			valid: true,
		},
		{
			name: "good serviceAccounts",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app":     "httpbin",
						"version": "v1",
					},
				},
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									ServiceAccounts: []string{"ns1/sa1", "ns2/sa2"},
								},
							},
							{
								Source: &security_beta.Source{
									Principals: []string{"sa2"},
								},
							},
						},
					},
				},
			},
			valid: true,
		},
		{
			name: "serviceAccounts and namespaces",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app":     "httpbin",
						"version": "v1",
					},
				},
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									ServiceAccounts: []string{"ns1/sa1", "ns2/sa2"},
									Namespaces:      []string{"ns"},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "custom-good",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_CUSTOM,
				ActionDetail: &security_beta.AuthorizationPolicy_Provider{
					Provider: &security_beta.AuthorizationPolicy_ExtensionProvider{
						Name: "my-custom-authz",
					},
				},
				Rules: []*security_beta.Rule{{}},
			},
			valid: true,
		},
		{
			name: "custom-empty-provider",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_CUSTOM,
				ActionDetail: &security_beta.AuthorizationPolicy_Provider{
					Provider: &security_beta.AuthorizationPolicy_ExtensionProvider{
						Name: "",
					},
				},
				Rules: []*security_beta.Rule{{}},
			},
			valid: false,
		},
		{
			name: "custom-nil-provider",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_CUSTOM,
				Rules:  []*security_beta.Rule{{}},
			},
			valid: false,
		},
		{
			name: "custom-invalid-rule",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_CUSTOM,
				ActionDetail: &security_beta.AuthorizationPolicy_Provider{
					Provider: &security_beta.AuthorizationPolicy_ExtensionProvider{
						Name: "my-custom-authz",
					},
				},
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									Namespaces:           []string{"ns"},
									NotNamespaces:        []string{"ns"},
									Principals:           []string{"id"},
									NotPrincipals:        []string{"id"},
									RequestPrincipals:    []string{"req"},
									NotRequestPrincipals: []string{"req"},
								},
							},
						},
						When: []*security_beta.Condition{
							{
								Key:       "source.namespace",
								Values:    []string{"source.namespace1"},
								NotValues: []string{"source.namespace2"},
							},
							{
								Key:       "source.principal",
								Values:    []string{"source.principal1"},
								NotValues: []string{"source.principal2"},
							},
							{
								Key:       "request.auth.claims[a]",
								Values:    []string{"claims1"},
								NotValues: []string{"claims2"},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "provider-wrong-action",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_ALLOW,
				ActionDetail: &security_beta.AuthorizationPolicy_Provider{
					Provider: &security_beta.AuthorizationPolicy_ExtensionProvider{
						Name: "",
					},
				},
				Rules: []*security_beta.Rule{{}},
			},
			valid: false,
		},
		{
			name: "allow-rules-nil",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_ALLOW,
			},
			valid: true,
		},
		{
			name:        "dry-run-valid-allow",
			annotations: map[string]string{"istio.io/dry-run": "true"},
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_ALLOW,
			},
			valid: true,
		},
		{
			name:        "dry-run-valid-deny",
			annotations: map[string]string{"istio.io/dry-run": "false"},
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_DENY,
				Rules:  []*security_beta.Rule{{}},
			},
			valid: true,
		},
		{
			name:        "dry-run-invalid-value",
			annotations: map[string]string{"istio.io/dry-run": "foo"},
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_ALLOW,
			},
			valid: false,
		},
		{
			name:        "dry-run-invalid-action-custom",
			annotations: map[string]string{"istio.io/dry-run": "true"},
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_CUSTOM,
			},
			valid: false,
		},
		{
			name:        "dry-run-invalid-action-audit",
			annotations: map[string]string{"istio.io/dry-run": "true"},
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_AUDIT,
			},
			valid: false,
		},
		{
			name: "deny-rules-nil",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_DENY,
			},
			valid: false,
		},
		{
			name: "selector-empty-value",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app":     "",
						"version": "v1",
					},
				},
			},
			valid: true,
		},
		{
			name: "selector-empty-key",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
						"":    "v1",
					},
				},
			},
			valid: false,
		},
		{
			name: "selector-empty-labels",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{},
			},
			valid:   true,
			Warning: true,
		},
		{
			name: "selector-wildcard-value",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin-*",
					},
				},
			},
			valid: false,
		},
		{
			name: "selector-wildcard-key",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app-*": "httpbin",
					},
				},
			},
			valid: false,
		},
		{
			name: "target-ref-good",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_DENY,
				TargetRef: &api.PolicyTargetReference{
					Group: gvk.KubernetesGateway.Group,
					Kind:  gvk.KubernetesGateway.Kind,
					Name:  "foo",
				},
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									Principals: []string{"temp"},
								},
							},
						},
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Ports:   []string{"8080"},
									Methods: []string{"GET", "DELETE"},
								},
							},
						},
					},
				},
			},
			valid:   true,
			Warning: false,
		},
		{
			name: "target-refs-good",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_DENY,
				TargetRefs: []*api.PolicyTargetReference{{
					Group: gvk.KubernetesGateway.Group,
					Kind:  gvk.KubernetesGateway.Kind,
					Name:  "foo",
				}},
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									Principals: []string{"temp"},
								},
							},
						},
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Ports:   []string{"8080"},
									Methods: []string{"GET", "DELETE"},
								},
							},
						},
					},
				},
			},
			valid:   true,
			Warning: false,
		},
		{
			name: "target-refs-good-service",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_DENY,
				TargetRefs: []*api.PolicyTargetReference{{
					Group: gvk.Service.Group,
					Kind:  gvk.Service.Kind,
					Name:  "foo",
				}},
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									Principals: []string{"temp"},
								},
							},
						},
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Ports:   []string{"8080"},
									Methods: []string{"GET", "DELETE"},
								},
							},
						},
					},
				},
			},
			valid:   true,
			Warning: false,
		},
		{
			name: "target-ref-non-empty-namespace",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_DENY,
				TargetRef: &api.PolicyTargetReference{
					Group:     gvk.KubernetesGateway.Group,
					Kind:      gvk.KubernetesGateway.Kind,
					Name:      "foo",
					Namespace: "bar",
				},
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									Principals: []string{"temp"},
								},
							},
						},
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Ports:   []string{"8080"},
									Methods: []string{"GET", "DELETE"},
								},
							},
						},
					},
				},
			},
			valid:   false,
			Warning: false,
		},
		{
			name: "target-ref-empty-name",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_DENY,
				TargetRef: &api.PolicyTargetReference{
					Group: gvk.KubernetesGateway.Group,
					Kind:  gvk.KubernetesGateway.Kind,
				},
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									Principals: []string{"temp"},
								},
							},
						},
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Ports:   []string{"8080"},
									Methods: []string{"GET", "DELETE"},
								},
							},
						},
					},
				},
			},
			valid:   false,
			Warning: false,
		},
		{
			name: "target-ref-wrong-group",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_DENY,
				TargetRef: &api.PolicyTargetReference{
					Group: "wrong-group",
					Kind:  gvk.KubernetesGateway.Kind,
					Name:  "foo",
				},
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									Principals: []string{"temp"},
								},
							},
						},
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Ports:   []string{"8080"},
									Methods: []string{"GET", "DELETE"},
								},
							},
						},
					},
				},
			},
			valid:   false,
			Warning: false,
		},
		{
			name: "target-ref-wrong-kind",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_DENY,
				TargetRef: &api.PolicyTargetReference{
					Group: gvk.KubernetesGateway.Group,
					Kind:  "wrong-kind",
					Name:  "foo",
				},
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									Principals: []string{"temp"},
								},
							},
						},
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Ports:   []string{"8080"},
									Methods: []string{"GET", "DELETE"},
								},
							},
						},
					},
				},
			},
			valid:   false,
			Warning: false,
		},
		{
			name: "target-ref-and-selector-cannot-both-be-set",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_DENY,
				TargetRef: &api.PolicyTargetReference{
					Group: gvk.KubernetesGateway.Group,
					Kind:  gvk.KubernetesGateway.Kind,
					Name:  "foo",
				},
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									Principals: []string{"temp"},
								},
							},
						},
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Ports:   []string{"8080"},
									Methods: []string{"GET", "DELETE"},
								},
							},
						},
					},
				},
			},
			valid:   false,
			Warning: false,
		},
		{
			name: "target-refs-and-selector-cannot-both-be-set",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_DENY,
				TargetRefs: []*api.PolicyTargetReference{{
					Group: gvk.KubernetesGateway.Group,
					Kind:  gvk.KubernetesGateway.Kind,
					Name:  "foo",
				}},
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									Principals: []string{"temp"},
								},
							},
						},
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Ports:   []string{"8080"},
									Methods: []string{"GET", "DELETE"},
								},
							},
						},
					},
				},
			},
			valid:   false,
			Warning: false,
		},
		{
			name: "from-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{},
					},
				},
			},
			valid: false,
		},
		{
			name: "source-nil",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "source-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "to-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{},
					},
				},
			},
			valid: false,
		},
		{
			name: "operation-nil",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "operation-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "Principals-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									Principals: []string{"p1", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "NotPrincipals-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									NotPrincipals: []string{"p1", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "RequestPrincipals-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									RequestPrincipals: []string{"p1", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "NotRequestPrincipals-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									NotRequestPrincipals: []string{"p1", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "Namespaces-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									Namespaces: []string{"ns", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "ServiceAccounts-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									ServiceAccounts: []string{"ns/sa", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "NotNamespaces-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									NotNamespaces: []string{"ns", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "IpBlocks-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									IpBlocks: []string{"1.2.3.4", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "NotIpBlocks-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									NotIpBlocks: []string{"1.2.3.4", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "RemoteIpBlocks-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									RemoteIpBlocks: []string{"1.2.3.4", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "NotRemoteIpBlocks-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									NotRemoteIpBlocks: []string{"1.2.3.4", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "Hosts-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Hosts: []string{"host", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "NotHosts-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									NotHosts: []string{"host", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "Ports-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Ports: []string{"80", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "NotPorts-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									NotPorts: []string{"80", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "Methods-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Methods: []string{"GET", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "NotMethods-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									NotMethods: []string{"GET", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "Paths-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Paths: []string{"/path", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "NotPaths-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									NotPaths: []string{"/path", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "value-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						When: []*security_beta.Condition{
							{
								Key:    "request.headers[:authority]",
								Values: []string{"v1", ""},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "invalid ip and port in ipBlocks",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									IpBlocks:    []string{"1.2.3.4", "ip1"},
									NotIpBlocks: []string{"5.6.7.8", "ip2"},
								},
							},
						},
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Ports:    []string{"80", "port1"},
									NotPorts: []string{"90", "port2"},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "invalid ip and port in remoteIpBlocks",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									RemoteIpBlocks:    []string{"1.2.3.4", "ip1"},
									NotRemoteIpBlocks: []string{"5.6.7.8", "ip2"},
								},
							},
						},
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Ports:    []string{"80", "port1"},
									NotPorts: []string{"90", "port2"},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "condition-key-missing",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
				Rules: []*security_beta.Rule{
					{
						When: []*security_beta.Condition{
							{
								Values: []string{"v1", "v2"},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "condition-key-empty",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
				Rules: []*security_beta.Rule{
					{
						When: []*security_beta.Condition{
							{
								Key:    "",
								Values: []string{"v1", "v2"},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "condition-value-missing",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
				Rules: []*security_beta.Rule{
					{
						When: []*security_beta.Condition{
							{
								Key: "source.principal",
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "condition-value-invalid",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						When: []*security_beta.Condition{
							{
								Key:    "source.ip",
								Values: []string{"a.b.c.d"},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "condition-notValue-invalid",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						When: []*security_beta.Condition{
							{
								Key:       "source.ip",
								NotValues: []string{"a.b.c.d"},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "condition-unknown",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
				Rules: []*security_beta.Rule{
					{
						When: []*security_beta.Condition{
							{
								Key:    "key1",
								Values: []string{"v1"},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "L7DenyWithFrom",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_DENY,
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									RequestPrincipals: []string{"example.com/sub-1"},
								},
							},
						},
					},
				},
			},
			valid:   true,
			Warning: true,
		},
		{
			name: "L7DenyWithTo",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_DENY,
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Methods: []string{"GET", "DELETE"},
								},
							},
						},
					},
				},
			},
			valid:   true,
			Warning: true,
		},
		{
			name: "L7DenyWithFromAndTo",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_DENY,
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									Principals: []string{"temp"},
								},
							},
						},
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Methods: []string{"GET", "DELETE"},
								},
							},
						},
					},
				},
			},
			valid:   true,
			Warning: true,
		},
		{
			name: "L7DenyWithFromAndToWithPort",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_DENY,
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									Principals: []string{"temp"},
								},
							},
						},
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Ports:   []string{"8080"},
									Methods: []string{"GET", "DELETE"},
								},
							},
						},
					},
				},
			},
			valid:   true,
			Warning: false,
		},
	}

	for _, c := range cases {
		war, got := ValidateAuthorizationPolicy(config.Config{
			Meta: config.Meta{
				Name:        "name",
				Namespace:   "namespace",
				Annotations: c.annotations,
			},
			Spec: c.in,
		})
		if (got == nil) != c.valid {
			t.Errorf("test: %q error: got: %v\nwant: %v", c.name, got, c.valid)
		}
		if (war != nil) != c.Warning {
			t.Errorf("test: %q warning: got: %v\nwant: %v", c.name, war, c.valid)
		}
	}
}

func TestValidateSidecar(t *testing.T) {
	tests := []struct {
		name  string
		in    *networking.Sidecar
		valid bool
		warn  bool
	}{
		{"empty ingress and egress", &networking.Sidecar{}, false, false},
		{"default", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, true, false},
		{"workload selector without labels", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
			WorkloadSelector: &networking.WorkloadSelector{},
		}, true, true},
		{"import local namespace with wildcard", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"./*"},
				},
			},
		}, true, false},
		{"import local namespace with fqdn", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"./foo.com"},
				},
			},
		}, true, false},
		{"import nothing", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"~/*"},
				},
			},
		}, true, false},
		{"bad egress host 1", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*"},
				},
			},
		}, false, false},
		{"bad egress host 2", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"/"},
				},
			},
		}, false, false},
		{"empty egress host", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{},
				},
			},
		}, false, false},
		{"multiple wildcard egress", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{
						"*/foo.com",
					},
				},
				{
					Hosts: []string{
						"ns1/bar.com",
					},
				},
			},
		}, false, false},
		{"wildcard egress not in end", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{
						"*/foo.com",
					},
				},
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   8080,
						Name:     "h8080",
					},
					Hosts: []string{
						"ns1/bar.com",
					},
				},
			},
		}, false, false},
		{"invalid Port", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http1",
						Number:   1000000,
						Name:     "",
					},
					Hosts: []string{
						"ns1/bar.com",
					},
				},
			},
		}, false, false},
		{"Port without name", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   8080,
					},
					Hosts: []string{
						"ns1/bar.com",
					},
				},
			},
		}, true, false},
		{"UDS bind in outbound", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   0,
						Name:     "uds",
					},
					Hosts: []string{
						"ns1/bar.com",
					},
					Bind: "unix:///@foo/bar/com",
				},
			},
		}, true, false},
		{"UDS bind in inbound", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   0,
						Name:     "uds",
					},
					Bind:            "unix:///@foo/bar/com",
					DefaultEndpoint: "127.0.0.1:9999",
				},
			},
		}, false, false},
		{"UDS bind in outbound 2", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   0,
						Name:     "uds",
					},
					Hosts: []string{
						"ns1/bar.com",
					},
					Bind: "unix:///foo/bar/com",
				},
			},
		}, true, false},
		{"invalid bind", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   0,
						Name:     "uds",
					},
					Hosts: []string{
						"ns1/bar.com",
					},
					Bind: "foobar:///@foo/bar/com",
				},
			},
		}, false, false},
		{"invalid capture mode with uds bind", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   0,
						Name:     "uds",
					},
					Hosts: []string{
						"ns1/bar.com",
					},
					Bind:        "unix:///@foo/bar/com",
					CaptureMode: networking.CaptureMode_IPTABLES,
				},
			},
		}, false, false},
		{"duplicate UDS bind", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   0,
						Name:     "uds",
					},
					Hosts: []string{
						"ns1/bar.com",
					},
					Bind: "unix:///@foo/bar/com",
				},
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   0,
						Name:     "uds",
					},
					Hosts: []string{
						"ns1/bar.com",
					},
					Bind: "unix:///@foo/bar/com",
				},
			},
		}, false, false},
		{"duplicate ports", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					Hosts: []string{
						"ns1/bar.com",
					},
				},
				{
					Port: &networking.SidecarPort{
						Protocol: "tcp",
						Number:   90,
						Name:     "tcp",
					},
					Hosts: []string{
						"ns2/bar.com",
					},
				},
			},
		}, false, false},
		{"ingress without port", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					DefaultEndpoint: "127.0.0.1:110",
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, false, false},
		{"ingress without port and with IPv6 endpoint", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					DefaultEndpoint: "[::1]:110",
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, false, false},
		{"ingress with duplicate ports", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "127.0.0.1:110",
				},
				{
					Port: &networking.SidecarPort{
						Protocol: "tcp",
						Number:   90,
						Name:     "bar",
					},
					DefaultEndpoint: "127.0.0.1:110",
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, false, false},
		{"ingress with duplicate ports and IPv6 endpoint", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "[::1]:110",
				},
				{
					Port: &networking.SidecarPort{
						Protocol: "tcp",
						Number:   90,
						Name:     "bar",
					},
					DefaultEndpoint: "[::1]:110",
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, false, false},
		{"ingress without default endpoint", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, true, false},
		{"ingress with invalid default endpoint in IPv4", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "1.1.1.1:90",
				},
			},
		}, false, false},
		{"ingress with invalid default endpoint in IPv6", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "[1:1:1:1:1:1:1:1]:90",
				},
			},
		}, false, false},
		{"ingress with invalid default endpoint uds", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "unix:///",
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, false, false},
		{"ingress with invalid default endpoint port in IPv4", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "127.0.0.1:hi",
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, false, false},
		{"ingress with invalid default endpoint port in IPv6", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "[::1]:hi",
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, false, false},
		{"valid ingress and egress in IPv4", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "127.0.0.1:9999",
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, true, false},
		{"valid ingress and egress in IPv6", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "[::1]:9999",
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, true, false},
		{"valid ingress and empty egress in IPv4", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "127.0.0.1:9999",
				},
			},
		}, true, false},
		{"valid ingress and empty egress in IPv6", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "[::1]:9999",
				},
			},
		}, true, false},
		{"empty", &networking.Sidecar{}, false, false},
		{"just outbound traffic policy", &networking.Sidecar{OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
			Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
		}}, true, false},
		{"empty protocol in IPv4", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Number: 90,
						Name:   "foo",
					},
					DefaultEndpoint: "127.0.0.1:9999",
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, true, false},
		{"empty protocol in IPv6", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Number: 90,
						Name:   "foo",
					},
					DefaultEndpoint: "[::1]:9999",
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, true, false},
		{"ALLOW_ANY sidecar egress policy with no egress proxy ", &networking.Sidecar{
			OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, true, false},
		{"sidecar egress proxy with RESGISTRY_ONLY(default)", &networking.Sidecar{
			OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				EgressProxy: &networking.Destination{
					Host:   "foo.bar",
					Subset: "shiny",
					Port: &networking.PortSelector{
						Number: 5000,
					},
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, false, false},
		{"sidecar egress proxy with ALLOW_ANY", &networking.Sidecar{
			OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
				EgressProxy: &networking.Destination{
					Host:   "foo.bar",
					Subset: "shiny",
					Port: &networking.PortSelector{
						Number: 5000,
					},
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, true, false},
		{"sidecar egress proxy with ALLOW_ANY, service hostname invalid fqdn", &networking.Sidecar{
			OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
				EgressProxy: &networking.Destination{
					Host:   "foobar*123",
					Subset: "shiny",
					Port: &networking.PortSelector{
						Number: 5000,
					},
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, false, false},
		{"sidecar egress proxy(without Port) with ALLOW_ANY", &networking.Sidecar{
			OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
				EgressProxy: &networking.Destination{
					Host:   "foo.bar",
					Subset: "shiny",
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, false, false},
		{"sidecar egress only one wildcarded", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{
						"*/*",
						"test/a.com",
					},
				},
			},
		}, true, true},
		{"sidecar egress wildcarded ns", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{
						"*/b.com",
						"test/a.com",
					},
				},
			},
		}, true, false},
		{"sidecar egress duplicated with wildcarded same namespace", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{
						"test/*",
						"test/a.com",
					},
				},
			},
		}, true, true},
		{"invalid partial wildcard", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{
						"test/*a.com",
					},
				},
			},
		}, false, false},
		{"sidecar egress duplicated with wildcarded same namespace .", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{
						"./*",
						"bar/a.com",
					},
				},
			},
		}, true, true},
		{"ingress tls mode set to ISTIO_MUTUAL in IPv4", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "127.0.0.1:9999",
					Tls: &networking.ServerTLSSettings{
						Mode: networking.ServerTLSSettings_ISTIO_MUTUAL,
					},
				},
			},
		}, false, false},
		{"ingress tls mode set to ISTIO_MUTUAL in IPv6", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "[::1]:9999",
					Tls: &networking.ServerTLSSettings{
						Mode: networking.ServerTLSSettings_ISTIO_MUTUAL,
					},
				},
			},
		}, false, false},
		{"ingress tls mode set to ISTIO_AUTO_PASSTHROUGH in IPv4", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "127.0.0.1:9999",
					Tls: &networking.ServerTLSSettings{
						Mode: networking.ServerTLSSettings_AUTO_PASSTHROUGH,
					},
				},
			},
		}, false, false},
		{"ingress tls mode set to ISTIO_AUTO_PASSTHROUGH in IPv6", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "[::1]:9999",
					Tls: &networking.ServerTLSSettings{
						Mode: networking.ServerTLSSettings_AUTO_PASSTHROUGH,
					},
				},
			},
		}, false, false},
		{"ingress tls invalid protocol in IPv4", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "tcp",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "127.0.0.1:9999",
					Tls: &networking.ServerTLSSettings{
						Mode: networking.ServerTLSSettings_SIMPLE,
					},
				},
			},
		}, false, false},
		{"ingress tls invalid protocol in IPv6", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "tcp",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "[::1]:9999",
					Tls: &networking.ServerTLSSettings{
						Mode: networking.ServerTLSSettings_SIMPLE,
					},
				},
			},
		}, false, false},
		{"ingress tls httpRedirect is not supported in IPv4", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "tcp",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "127.0.0.1:9999",
					Tls: &networking.ServerTLSSettings{
						Mode:          networking.ServerTLSSettings_SIMPLE,
						HttpsRedirect: true,
					},
				},
			},
		}, false, false},
		{"ingress tls httpRedirect is not supported in IPv6", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "tcp",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "[::1]:9999",
					Tls: &networking.ServerTLSSettings{
						Mode:          networking.ServerTLSSettings_SIMPLE,
						HttpsRedirect: true,
					},
				},
			},
		}, false, false},
		{"ingress tls SAN entries are not supported in IPv4", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "tcp",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "127.0.0.1:9999",
					Tls: &networking.ServerTLSSettings{
						Mode:            networking.ServerTLSSettings_SIMPLE,
						SubjectAltNames: []string{"httpbin.com"},
					},
				},
			},
		}, false, false},
		{"ingress tls SAN entries are not supported in IPv6", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "tcp",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "[::1]:9999",
					Tls: &networking.ServerTLSSettings{
						Mode:            networking.ServerTLSSettings_SIMPLE,
						SubjectAltNames: []string{"httpbin.com"},
					},
				},
			},
		}, false, false},
		{"ingress tls credentialName is not supported in IPv4", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "tcp",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "127.0.0.1:9999",
					Tls: &networking.ServerTLSSettings{
						Mode:           networking.ServerTLSSettings_SIMPLE,
						CredentialName: "secret-name",
					},
				},
			},
		}, false, false},
		{"ingress tls credentialName is not supported in IPv6", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "tcp",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "[::1]:9999",
					Tls: &networking.ServerTLSSettings{
						Mode:           networking.ServerTLSSettings_SIMPLE,
						CredentialName: "secret-name",
					},
				},
			},
		}, false, false},
		{"ingress tls credentialNames is not supported in IPv4", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "tcp",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "127.0.0.1:9999",
					Tls: &networking.ServerTLSSettings{
						Mode:            networking.ServerTLSSettings_SIMPLE,
						CredentialNames: []string{"secret-name"},
					},
				},
			},
		}, false, false},
		{"ingress tls credentialNames is not supported in IPv6", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "tcp",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "[::1]:9999",
					Tls: &networking.ServerTLSSettings{
						Mode:            networking.ServerTLSSettings_SIMPLE,
						CredentialNames: []string{"secret-name"},
					},
				},
			},
		}, false, false},
		{"ingress tls tlsCertificates is not supported in IPv4", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "tcp",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "127.0.0.1:9999",
					Tls: &networking.ServerTLSSettings{
						Mode: networking.ServerTLSSettings_SIMPLE,
						TlsCertificates: []*networking.ServerTLSSettings_TLSCertificate{
							{
								ServerCertificate: "/etc/istio/ingress-certs/tls.crt",
								PrivateKey:        "/etc/istio/ingress-certs/tls.key",
							},
							{
								ServerCertificate: "/etc/istio/ingress-certs/tls2.crt",
								PrivateKey:        "/etc/istio/ingress-certs/tls2.key",
							},
						},
					},
				},
			},
		}, false, false},
		{"ingress tls tlsCertificates is not supported in IPv6", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "tcp",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "[::1]:9999",
					Tls: &networking.ServerTLSSettings{
						Mode: networking.ServerTLSSettings_SIMPLE,
						TlsCertificates: []*networking.ServerTLSSettings_TLSCertificate{
							{
								ServerCertificate: "/etc/istio/ingress-certs/tls.crt",
								PrivateKey:        "/etc/istio/ingress-certs/tls.key",
							},
							{
								ServerCertificate: "/etc/istio/ingress-certs/tls2.crt",
								PrivateKey:        "/etc/istio/ingress-certs/tls2.key",
							},
						},
					},
				},
			},
		}, false, false},
		// We're using the same validation code as DestinationRule, so we're really trusting the TrafficPolicy
		// validation code's testing. Here we just want to exercise the edge cases around Sidecar specifically.
		{"valid inbound connection pool", &networking.Sidecar{
			InboundConnectionPool: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					Http1MaxPendingRequests:  1024,
					Http2MaxRequests:         1024,
					MaxRequestsPerConnection: 1024,
					MaxRetries:               1024,
				},
			},
		}, true, false},
		{"valid port-level connection pool with top level default", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					ConnectionPool: &networking.ConnectionPoolSettings{
						Http: &networking.ConnectionPoolSettings_HTTPSettings{
							Http1MaxPendingRequests:  1024,
							Http2MaxRequests:         1024,
							MaxRequestsPerConnection: 1024,
							MaxRetries:               1024,
						},
					},
				},
			},
			InboundConnectionPool: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					Http1MaxPendingRequests:  1024,
					Http2MaxRequests:         1024,
					MaxRequestsPerConnection: 1024,
					MaxRetries:               1024,
				},
			},
		}, true, false},
		{"valid port-level connection pool without a top level default", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					ConnectionPool: &networking.ConnectionPoolSettings{
						Http: &networking.ConnectionPoolSettings_HTTPSettings{
							Http1MaxPendingRequests:  1024,
							Http2MaxRequests:         1024,
							MaxRequestsPerConnection: 1024,
							MaxRetries:               1024,
						},
					},
				},
			},
		}, true, false},
		{"warn http connection settings on tcp port", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "tcp",
						Number:   90,
						Name:     "foo",
					},
					ConnectionPool: &networking.ConnectionPoolSettings{
						Http: &networking.ConnectionPoolSettings_HTTPSettings{
							Http1MaxPendingRequests:  1024,
							Http2MaxRequests:         1024,
							MaxRequestsPerConnection: 1024,
							MaxRetries:               1024,
						},
					},
				},
			},
		}, true, true},
		{"invalid top level connection pool", &networking.Sidecar{
			InboundConnectionPool: &networking.ConnectionPoolSettings{},
		}, false, false},
		{"invalid port-level connection pool", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.SidecarPort{
						Protocol: "tcp",
						Number:   90,
						Name:     "foo",
					},
					ConnectionPool: &networking.ConnectionPoolSettings{},
				},
			},
		}, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warn, err := ValidateSidecar(config.Config{
				Meta: config.Meta{
					Name:      "foo",
					Namespace: "bar",
				},
				Spec: tt.in,
			})
			checkValidation(t, warn, err, tt.valid, tt.warn)
		})
	}
}

func TestValidateRequestAuthentication(t *testing.T) {
	cases := []struct {
		name        string
		configName  string
		annotations map[string]string
		in          proto.Message
		valid       bool
		warning     bool
	}{
		{
			name:       "empty spec",
			configName: constants.DefaultAuthenticationPolicyName,
			in:         &security_beta.RequestAuthentication{},
			valid:      true,
		},
		{
			name:       "another empty spec",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.RequestAuthentication{
				Selector: &api.WorkloadSelector{},
			},
			valid:   true,
			warning: true,
		},
		{
			name:       "empty spec with non default name",
			configName: someName,
			in:         &security_beta.RequestAuthentication{},
			valid:      true,
		},
		{
			name:        "dry run annotation not supported",
			configName:  someName,
			annotations: map[string]string{"istio.io/dry-run": "true"},
			in:          &security_beta.RequestAuthentication{},
			valid:       false,
		},
		{
			name:       "default name with non empty selector",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.RequestAuthentication{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
			},
			valid: true,
		},
		{
			name:       "empty jwt rule",
			configName: "foo",
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{},
				},
			},
			valid: false,
		},
		{
			name:       "empty issuer",
			configName: "foo",
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer: "",
					},
				},
			},
			valid: false,
		},
		{
			name:       "bad JwksUri - no protocol",
			configName: "foo",
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "foo.com",
					},
				},
			},
			valid: false,
		},
		{
			name:       "bad JwksUri - invalid port",
			configName: "foo",
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "https://foo.com:not-a-number",
					},
				},
			},
			valid: false,
		},
		{
			name:       "empty value",
			configName: "foo",
			in: &security_beta.RequestAuthentication{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app":     "httpbin",
						"version": "",
					},
				},
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "https://foo.com/cert",
					},
				},
			},
			valid: true,
		},
		{
			name:       "bad workload selector - empty key",
			configName: "foo",
			in: &security_beta.RequestAuthentication{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
						"":    "v1",
					},
				},
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "https://foo.com/cert",
					},
				},
			},
			valid: false,
		},
		{
			name:       "good targetRef",
			configName: "foo",
			in: &security_beta.RequestAuthentication{
				TargetRef: &api.PolicyTargetReference{
					Group: gvk.KubernetesGateway.Group,
					Kind:  gvk.KubernetesGateway.Kind,
					Name:  "foo",
				},
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "https://foo.com/cert",
					},
				},
			},
			valid: true,
		},
		{
			name:       "bad targetRef - empty name",
			configName: "foo",
			in: &security_beta.RequestAuthentication{
				TargetRef: &api.PolicyTargetReference{
					Group: gvk.KubernetesGateway.Group,
					Kind:  gvk.KubernetesGateway.Kind,
				},
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "https://foo.com/cert",
					},
				},
			},
			valid: false,
		},
		{
			name:       "bad targetRef - non-empty namespace",
			configName: "foo",
			in: &security_beta.RequestAuthentication{
				TargetRef: &api.PolicyTargetReference{
					Group:     gvk.KubernetesGateway.Group,
					Kind:      gvk.KubernetesGateway.Kind,
					Name:      "foo",
					Namespace: "bar",
				},
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "https://foo.com/cert",
					},
				},
			},
			valid: false,
		},
		{
			name:       "bad targetRef - wrong group",
			configName: "foo",
			in: &security_beta.RequestAuthentication{
				TargetRef: &api.PolicyTargetReference{
					Group: "wrong-group",
					Kind:  gvk.KubernetesGateway.Kind,
					Name:  "foo",
				},
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "https://foo.com/cert",
					},
				},
			},
			valid: false,
		},
		{
			name:       "bad targetRef - wrong kind",
			configName: "foo",
			in: &security_beta.RequestAuthentication{
				TargetRef: &api.PolicyTargetReference{
					Group: gvk.KubernetesGateway.Group,
					Kind:  "wrong-kind",
					Name:  "foo",
				},
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "https://foo.com/cert",
					},
				},
			},
			valid: false,
		},
		{
			name:       "targetRef and selector cannot both be set",
			configName: "foo",
			in: &security_beta.RequestAuthentication{
				TargetRef: &api.PolicyTargetReference{
					Group: gvk.KubernetesGateway.Group,
					Kind:  gvk.KubernetesGateway.Kind,
					Name:  "foo",
				},
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "https://foo.com/cert",
					},
				},
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
			},
			valid: false,
		},
		{
			name:       "bad header location",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "https://foo.com",
						FromHeaders: []*security_beta.JWTHeader{
							{
								Name:   "",
								Prefix: "Bearer ",
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name:       "bad param location",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:     "foo.com",
						JwksUri:    "https://foo.com",
						FromParams: []string{""},
					},
				},
			},
			valid: false,
		},
		{
			name:       "good",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "https://foo.com",
						FromHeaders: []*security_beta.JWTHeader{
							{
								Name:   "x-foo",
								Prefix: "Bearer ",
							},
						},
					},
				},
			},
			valid: true,
		},
		{
			name:       "bad cookie location",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:      "foo.com",
						JwksUri:     "https://foo.com",
						FromCookies: []string{"", "foo"},
					},
				},
			},
			valid: false,
		},
		{
			name:       "jwks ok",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer: "foo.com",
						Jwks:   "{ \"keys\":[ {\"e\":\"AQAB\",\"kid\":\"DHFbpoIUqrY8t2zpA2qXfCmr5VO5ZEr4RzHU_-envvQ\",\"kty\":\"RSA\",\"n\":\"xAE7eB6qugXyCAG3yhh7pkDkT65pHymX-P7KfIupjf59vsdo91bSP9C8H07pSAGQO1MV_xFj9VswgsCg4R6otmg5PV2He95lZdHtOcU5DXIg_pbhLdKXbi66GlVeK6ABZOUW3WYtnNHD-91gVuoeJT_DwtGGcp4ignkgXfkiEm4sw-4sfb4qdt5oLbyVpmW6x9cfa7vs2WTfURiCrBoUqgBo_-4WTiULmmHSGZHOjzwa8WtrtOQGsAFjIbno85jp6MnGGGZPYZbDAa_b3y5u-YpW7ypZrvD8BgtKVjgtQgZhLAGezMt0ua3DRrWnKqTZ0BJ_EyxOGuHJrLsn00fnMQ\"}]}", // nolint: lll
						FromHeaders: []*security_beta.JWTHeader{
							{
								Name:   "x-foo",
								Prefix: "Bearer ",
							},
						},
					},
				},
			},
			valid: true,
		},
		{
			name:       "jwks error",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer: "foo.com",
						Jwks:   "foo",
						FromHeaders: []*security_beta.JWTHeader{
							{
								Name:   "x-foo",
								Prefix: "Bearer ",
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name:       "null outputClaimToHeader",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:               "foo.com",
						JwksUri:              "https://foo.com",
						OutputClaimToHeaders: []*security_beta.ClaimToHeader{{}},
					},
				},
			},
			valid: false,
		},
		{
			name:       "null claim value in outputClaimToHeader ",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "https://foo.com",
						OutputClaimToHeaders: []*security_beta.ClaimToHeader{
							{
								Header: "x-jwt-claim",
								Claim:  "",
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name:       "null header value in outputClaimToHeader ",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "https://foo.com",
						OutputClaimToHeaders: []*security_beta.ClaimToHeader{
							{
								Header: "",
								Claim:  "sub",
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name:       "invalid header value in outputClaimToHeader ",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "https://foo.com",
						OutputClaimToHeaders: []*security_beta.ClaimToHeader{
							{
								Header: "abc%123",
								Claim:  "sub",
							},
						},
					},
				},
			},
			valid: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			warn, got := ValidateRequestAuthentication(config.Config{
				Meta: config.Meta{
					Name:        c.configName,
					Namespace:   someNamespace,
					Annotations: c.annotations,
				},
				Spec: c.in,
			})
			if (got == nil) != c.valid {
				t.Errorf("test: %q got(%v) != want(%v)\n", c.name, got, c.valid)
			}
			if (warn == nil) == c.warning {
				t.Errorf("test: %q warn(%v) != want(%v)\n", c.name, warn, c.warning)
			}
		})
	}
}

func TestValidatePeerAuthentication(t *testing.T) {
	cases := []struct {
		name       string
		configName string
		in         proto.Message
		valid      bool
		warning    bool
	}{
		{
			name:       "empty spec",
			configName: constants.DefaultAuthenticationPolicyName,
			in:         &security_beta.PeerAuthentication{},
			valid:      true,
		},
		{
			name:       "empty mtls with selector of empty labels",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.PeerAuthentication{
				Selector: &api.WorkloadSelector{},
			},
			valid:   true,
			warning: true,
		},
		{
			name:       "empty spec with non default name",
			configName: someName,
			in:         &security_beta.PeerAuthentication{},
			valid:      true,
		},
		{
			name:       "default name with non empty selector",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.PeerAuthentication{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
			},
			valid: true,
		},
		{
			name:       "empty port level mtls",
			configName: "foo",
			in: &security_beta.PeerAuthentication{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
				PortLevelMtls: map[uint32]*security_beta.PeerAuthentication_MutualTLS{},
			},
			valid: false,
		},
		{
			name:       "empty selector with port level mtls",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.PeerAuthentication{
				PortLevelMtls: map[uint32]*security_beta.PeerAuthentication_MutualTLS{
					8080: {
						Mode: security_beta.PeerAuthentication_MutualTLS_UNSET,
					},
				},
			},
			valid: false,
		},
		{
			name:       "port 0",
			configName: "foo",
			in: &security_beta.PeerAuthentication{
				PortLevelMtls: map[uint32]*security_beta.PeerAuthentication_MutualTLS{
					0: {
						Mode: security_beta.PeerAuthentication_MutualTLS_UNSET,
					},
				},
			},
			valid: false,
		},
		{
			name:       "unset mode",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.PeerAuthentication{
				Mtls: &security_beta.PeerAuthentication_MutualTLS{
					Mode: security_beta.PeerAuthentication_MutualTLS_UNSET,
				},
			},
			valid: true,
		},
		{
			name:       "port level",
			configName: "port-level",
			in: &security_beta.PeerAuthentication{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
				PortLevelMtls: map[uint32]*security_beta.PeerAuthentication_MutualTLS{
					8080: {
						Mode: security_beta.PeerAuthentication_MutualTLS_UNSET,
					},
				},
			},
			valid: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			warn, got := ValidatePeerAuthentication(config.Config{
				Meta: config.Meta{
					Name:      c.configName,
					Namespace: someNamespace,
				},
				Spec: c.in,
			})
			if (got == nil) != c.valid {
				t.Errorf("got(%v) != want(%v)\n", got, c.valid)
			}
			if (warn == nil) == c.warning {
				t.Errorf("warn(%v) != want(%v)\n", warn, c.warning)
			}
		})
	}
}

func Test_validateExportTo(t *testing.T) {
	tests := []struct {
		name                          string
		namespace                     string
		exportTo                      []string
		isDestinationRuleWithSelector bool
		isServiceEntry                bool
		wantErr                       bool
	}{
		{
			name:      "empty exportTo is okay",
			namespace: "ns5",
			wantErr:   false,
		},
		{
			name:      "* is allowed",
			namespace: "ns5",
			exportTo:  []string{"*"},
			wantErr:   false,
		},
		{
			name:      ". and ns1 are allowed",
			namespace: "ns5",
			exportTo:  []string{".", "ns1"},
			wantErr:   false,
		},
		{
			name:      "bunch of namespaces in exportTo is okay",
			namespace: "ns5",
			exportTo:  []string{"ns1", "ns2", "ns5"},
			wantErr:   false,
		},
		{
			name:           "~ is allowed for service entry configs",
			namespace:      "ns5",
			exportTo:       []string{"~"},
			isServiceEntry: true,
			wantErr:        false,
		},
		{
			name:      "~ not allowed for non service entry configs",
			namespace: "ns5",
			exportTo:  []string{"~", "ns1"},
			wantErr:   true,
		},
		{
			name:      ". and * together are not allowed",
			namespace: "ns5",
			exportTo:  []string{".", "*"},
			wantErr:   true,
		},
		{
			name:      "* and ns1 together are not allowed",
			namespace: "ns5",
			exportTo:  []string{"*", "ns1"},
			wantErr:   true,
		},
		{
			name:      ". and same namespace in exportTo is not okay",
			namespace: "ns5",
			exportTo:  []string{".", "ns5"},
			wantErr:   true,
		},
		{
			name:      "duplicate namespaces in exportTo is not okay",
			namespace: "ns5",
			exportTo:  []string{"ns1", "ns2", "ns1"},
			wantErr:   true,
		},
		{
			name:           "duplicate none in service entry exportTo is not okay",
			namespace:      "ns5",
			exportTo:       []string{"~", "~", "ns1"},
			isServiceEntry: true,
			wantErr:        true,
		},
		{
			name:      "invalid namespace names are not okay",
			namespace: "ns5",
			exportTo:  []string{"ns1_"},
			wantErr:   true,
		},
		{
			name:           "none and other namespaces cannot be combined in service entry exportTo",
			namespace:      "ns5",
			exportTo:       []string{"~", "ns1"},
			isServiceEntry: true,
			wantErr:        true,
		},
		{
			name:                          "destination rule with workloadselector cannot have exportTo (*)",
			namespace:                     "ns5",
			exportTo:                      []string{"*"},
			isServiceEntry:                false,
			isDestinationRuleWithSelector: true,
			wantErr:                       true,
		},
		{
			name:                          "destination rule with workloadselector can have only exportTo (.)",
			namespace:                     "ns5",
			exportTo:                      []string{"."},
			isServiceEntry:                false,
			isDestinationRuleWithSelector: true,
			wantErr:                       false,
		},
		{
			name:                          "destination rule with workloadselector cannot have another ns in exportTo (.)",
			namespace:                     "ns5",
			exportTo:                      []string{"somens"},
			isServiceEntry:                false,
			isDestinationRuleWithSelector: true,
			wantErr:                       true,
		},
		{
			name:                          "destination rule with workloadselector cannot have another ns in addition to own ns in exportTo (.)",
			namespace:                     "ns5",
			exportTo:                      []string{".", "somens"},
			isServiceEntry:                false,
			isDestinationRuleWithSelector: true,
			wantErr:                       true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateExportTo(tt.namespace, tt.exportTo, tt.isServiceEntry, tt.isDestinationRuleWithSelector); (err != nil) != tt.wantErr {
				t.Errorf("validateExportTo() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateTelemetry(t *testing.T) {
	tests := []struct {
		name    string
		in      proto.Message
		err     string
		warning string
	}{
		{"empty", &telemetry.Telemetry{}, "", ""},
		{"invalid message", &networking.Server{}, "cannot cast", ""},
		{
			"multiple providers",
			&telemetry.Telemetry{
				Tracing: []*telemetry.Tracing{{
					Providers: []*telemetry.ProviderRef{
						{Name: "a"},
						{Name: "b"},
					},
				}},
			},
			"", "multiple providers",
		},
		{
			"multiple tracers",
			&telemetry.Telemetry{
				Tracing: []*telemetry.Tracing{{}, {}},
			},
			"", "multiple tracing",
		},
		{
			"bad randomSamplingPercentage",
			&telemetry.Telemetry{
				Tracing: []*telemetry.Tracing{{
					RandomSamplingPercentage: &wrapperspb.DoubleValue{Value: 101},
				}},
			},
			"randomSamplingPercentage", "",
		},
		{
			"tracing with a good custom tag",
			&telemetry.Telemetry{
				Tracing: []*telemetry.Tracing{{
					CustomTags: map[string]*telemetry.Tracing_CustomTag{
						"clusterID": {
							Type: &telemetry.Tracing_CustomTag_Environment{
								Environment: &telemetry.Tracing_Environment{
									Name: "FOO",
								},
							},
						},
					},
				}},
			},
			"", "",
		},
		{
			"tracing with a nil custom tag",
			&telemetry.Telemetry{
				Tracing: []*telemetry.Tracing{{
					CustomTags: map[string]*telemetry.Tracing_CustomTag{
						"clusterID": nil,
					},
				}},
			},
			"tag 'clusterID' may not have a nil value", "",
		},
		{
			"bad metrics operation",
			&telemetry.Telemetry{
				Metrics: []*telemetry.Metrics{{
					Overrides: []*telemetry.MetricsOverrides{
						{
							TagOverrides: map[string]*telemetry.MetricsOverrides_TagOverride{
								"my-tag": {
									Operation: telemetry.MetricsOverrides_TagOverride_UPSERT,
									Value:     "",
								},
							},
						},
					},
				}},
			},
			"must be set when operation is UPSERT", "",
		},
		{
			"good metrics operation",
			&telemetry.Telemetry{
				Metrics: []*telemetry.Metrics{{
					Overrides: []*telemetry.MetricsOverrides{
						{
							TagOverrides: map[string]*telemetry.MetricsOverrides_TagOverride{
								"my-tag": {
									Operation: telemetry.MetricsOverrides_TagOverride_UPSERT,
									Value:     "some-cel-expression",
								},
							},
						},
					},
				}},
			},
			"", "",
		},
		{
			"multi-accessloggings",
			&telemetry.Telemetry{
				AccessLogging: []*telemetry.AccessLogging{
					{
						Providers: []*telemetry.ProviderRef{
							{
								Name: "envoy",
							},
						},
					},
					{
						Providers: []*telemetry.ProviderRef{
							{
								Name: "otel",
							},
						},
					},
				},
			},
			"", "",
		},
		{
			"multi-accesslogging-providers",
			&telemetry.Telemetry{
				AccessLogging: []*telemetry.AccessLogging{
					{
						Providers: []*telemetry.ProviderRef{
							{
								Name: "envoy",
							},
							{
								Name: "otel",
							},
						},
					},
				},
			},
			"", "",
		},
		{
			"good targetRef",
			&telemetry.Telemetry{
				Tracing: []*telemetry.Tracing{{
					CustomTags: map[string]*telemetry.Tracing_CustomTag{
						"clusterID": {
							Type: &telemetry.Tracing_CustomTag_Environment{
								Environment: &telemetry.Tracing_Environment{
									Name: "FOO",
								},
							},
						},
					},
				}},
				TargetRef: &api.PolicyTargetReference{
					Group: gvk.KubernetesGateway.Group,
					Kind:  gvk.KubernetesGateway.Kind,
					Name:  "foo",
				},
			},
			"", "",
		},
		{
			"bad targetRef - empty name",
			&telemetry.Telemetry{
				Tracing: []*telemetry.Tracing{{
					CustomTags: map[string]*telemetry.Tracing_CustomTag{
						"clusterID": {
							Type: &telemetry.Tracing_CustomTag_Environment{
								Environment: &telemetry.Tracing_Environment{
									Name: "FOO",
								},
							},
						},
					},
				}},
				TargetRef: &api.PolicyTargetReference{
					Group: gvk.KubernetesGateway.Group,
					Kind:  gvk.KubernetesGateway.Kind,
				},
			},
			"targetRef name must be set", "",
		},
		{
			"bad targetRef - non-empty namespace",
			&telemetry.Telemetry{
				Tracing: []*telemetry.Tracing{{
					CustomTags: map[string]*telemetry.Tracing_CustomTag{
						"clusterID": {
							Type: &telemetry.Tracing_CustomTag_Environment{
								Environment: &telemetry.Tracing_Environment{
									Name: "FOO",
								},
							},
						},
					},
				}},
				TargetRef: &api.PolicyTargetReference{
					Group:     gvk.KubernetesGateway.Group,
					Kind:      gvk.KubernetesGateway.Kind,
					Name:      "foo",
					Namespace: "bar",
				},
			},
			"targetRef namespace must not be set", "",
		},
		{
			"bad targetRef - wrong group",
			&telemetry.Telemetry{
				Tracing: []*telemetry.Tracing{{
					CustomTags: map[string]*telemetry.Tracing_CustomTag{
						"clusterID": {
							Type: &telemetry.Tracing_CustomTag_Environment{
								Environment: &telemetry.Tracing_Environment{
									Name: "FOO",
								},
							},
						},
					},
				}},
				TargetRef: &api.PolicyTargetReference{
					Group: "wrong-group",
					Kind:  gvk.KubernetesGateway.Kind,
					Name:  "foo",
				},
			},
			fmt.Sprintf("targetRef must be to one of %v but was %s/%s",
				allowedTargetRefs, "wrong-group", gvk.KubernetesGateway.Kind), "",
		},
		{
			"bad targetRef - wrong kind",
			&telemetry.Telemetry{
				Tracing: []*telemetry.Tracing{{
					CustomTags: map[string]*telemetry.Tracing_CustomTag{
						"clusterID": {
							Type: &telemetry.Tracing_CustomTag_Environment{
								Environment: &telemetry.Tracing_Environment{
									Name: "FOO",
								},
							},
						},
					},
				}},
				TargetRef: &api.PolicyTargetReference{
					Group: gvk.KubernetesGateway.Group,
					Kind:  "wrong-kind",
					Name:  "foo",
				},
			},
			fmt.Sprintf("targetRef must be to one of %v but was %s/%s",
				allowedTargetRefs, gvk.KubernetesGateway.Group, "wrong-kind"), "",
		},
		{
			"targetRef and selector cannot both be set",
			&telemetry.Telemetry{
				Tracing: []*telemetry.Tracing{{
					CustomTags: map[string]*telemetry.Tracing_CustomTag{
						"clusterID": {
							Type: &telemetry.Tracing_CustomTag_Environment{
								Environment: &telemetry.Tracing_Environment{
									Name: "FOO",
								},
							},
						},
					},
				}},
				TargetRef: &api.PolicyTargetReference{
					Group: gvk.KubernetesGateway.Group,
					Kind:  "wrong-kind",
					Name:  "foo",
				},
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
			},
			"only one of targetRefs or workloadSelector can be set", "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warn, err := ValidateTelemetry(config.Config{
				Meta: config.Meta{
					Name:      someName,
					Namespace: someNamespace,
				},
				Spec: tt.in,
			})
			checkValidationMessage(t, warn, err, tt.warning, tt.err)
		})
	}
}

func TestValidateProxyConfig(t *testing.T) {
	tests := []struct {
		name    string
		in      proto.Message
		out     string
		warning string
	}{
		{"empty", &networkingv1beta1.ProxyConfig{}, "", ""},
		{name: "invalid concurrency", in: &networkingv1beta1.ProxyConfig{
			Concurrency: &wrapperspb.Int32Value{Value: -1},
		}, out: "concurrency must be greater than or equal to 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warn, err := ValidateProxyConfig(config.Config{
				Meta: config.Meta{
					Name:      someName,
					Namespace: someNamespace,
				},
				Spec: tt.in,
			})
			checkValidationMessage(t, warn, err, tt.warning, tt.out)
		})
	}
}

func TestValidateTelemetryFilter(t *testing.T) {
	cases := []struct {
		filter *telemetry.AccessLogging_Filter
		valid  bool
	}{
		{
			filter: &telemetry.AccessLogging_Filter{
				Expression: "response.code >= 400",
			},
			valid: true,
		},
		{
			filter: &telemetry.AccessLogging_Filter{
				Expression: "connection.mtls && request.url_path.contains('v1beta3')",
			},
			valid: true,
		},
		{
			filter: &telemetry.AccessLogging_Filter{
				// TODO: find a better way to verify this
				// this should be an invalid expression
				Expression: "response.code",
			},
			valid: true,
		},
		{
			filter: &telemetry.AccessLogging_Filter{
				Expression: ")++++",
			},
			valid: false,
		},
	}

	for _, tc := range cases {
		t.Run("", func(t *testing.T) {
			err := validateTelemetryFilter(tc.filter)
			errFound := err != nil
			if tc.valid && errFound {
				t.Errorf("validateTelemetryFilter(%v) produced unexpected error: %v", tc.filter, err)
			}
			if !tc.valid && !errFound {
				t.Errorf("validateTelemetryFilter(%v) did not produce expected error", tc.filter)
			}
		})
	}
}

func TestValidateWasmPlugin(t *testing.T) {
	tests := []struct {
		name    string
		in      proto.Message
		out     string
		warning string
	}{
		{"empty", &extensions.WasmPlugin{}, "url field needs to be set", ""},
		{"invalid message", &networking.Server{}, "cannot cast", ""},
		{
			"wrong scheme",
			&extensions.WasmPlugin{
				Url: "ftp://test.com/test",
			},
			"unsupported scheme", "",
		},
		{
			"valid http",
			&extensions.WasmPlugin{
				Url: "http://test.com/test",
			},
			"", "",
		},
		{
			"valid http w/ sha",
			&extensions.WasmPlugin{
				Url:    "http://test.com/test",
				Sha256: "01ba4719c80b6fe911b091a7c05124b64eeece964e09c058ef8f9805daca546b",
			},
			"", "",
		},
		{
			"short sha",
			&extensions.WasmPlugin{
				Url:    "http://test.com/test",
				Sha256: "01ba47",
			},
			"sha256 field must be 64 characters long", "",
		},
		{
			"invalid sha",
			&extensions.WasmPlugin{
				Url:    "http://test.com/test",
				Sha256: "test",
			},
			"sha256 field must be 64 characters long", "",
		},
		{
			"invalid sha characters",
			&extensions.WasmPlugin{
				Url:    "http://test.com/test",
				Sha256: "01Ba4719c80b6fe911b091a7c05124b64eeece964e09c058ef8f9805daca546b",
			},
			"sha256 field must match [a-f0-9]{64} pattern", "",
		},
		{
			"valid oci",
			&extensions.WasmPlugin{
				Url: "oci://test.com/test",
			},
			"", "",
		},
		{
			"valid oci no scheme",
			&extensions.WasmPlugin{
				Url: "test.com/test",
			},
			"", "",
		},
		{
			"invalid vm config - invalid env name",
			&extensions.WasmPlugin{
				Url: "test.com/test",
				VmConfig: &extensions.VmConfig{
					Env: []*extensions.EnvVar{
						{
							Name:      "",
							ValueFrom: extensions.EnvValueSource_HOST,
						},
					},
				},
			},
			"spec.vmConfig.env invalid", "",
		},
		{
			"invalid vm config - duplicate env",
			&extensions.WasmPlugin{
				Url: "test.com/test",
				VmConfig: &extensions.VmConfig{
					Env: []*extensions.EnvVar{
						{
							Name:  "ENV1",
							Value: "VAL1",
						},
						{
							Name:  "ENV1",
							Value: "VAL1",
						},
					},
				},
			},
			"duplicate env", "",
		},
		{
			"target-ref-good",
			&extensions.WasmPlugin{
				Url: "http://test.com/test",
				TargetRef: &api.PolicyTargetReference{
					Group: gvk.KubernetesGateway.Group,
					Kind:  gvk.KubernetesGateway.Kind,
					Name:  "foo",
				},
			},
			"", "",
		},
		{
			"target-ref-good-service",
			&extensions.WasmPlugin{
				Url: "http://test.com/test",
				TargetRef: &api.PolicyTargetReference{
					Group: gvk.Service.Group,
					Kind:  gvk.Service.Kind,
					Name:  "foo",
				},
			},
			"", "",
		},
		{
			"target-ref-non-empty-namespace",
			&extensions.WasmPlugin{
				Url: "http://test.com/test",
				TargetRef: &api.PolicyTargetReference{
					Group:     gvk.KubernetesGateway.Group,
					Kind:      gvk.KubernetesGateway.Kind,
					Name:      "foo",
					Namespace: "bar",
				},
			},
			"targetRef namespace must not be set", "",
		},
		{
			"target-ref-empty-name",
			&extensions.WasmPlugin{
				Url: "http://test.com/test",
				TargetRef: &api.PolicyTargetReference{
					Group: gvk.KubernetesGateway.Group,
					Kind:  gvk.KubernetesGateway.Kind,
				},
			},
			"targetRef name must be set", "",
		},
		{
			"target-ref-wrong-group",
			&extensions.WasmPlugin{
				Url: "http://test.com/test",
				TargetRef: &api.PolicyTargetReference{
					Group: "wrong-group",
					Kind:  gvk.KubernetesGateway.Kind,
					Name:  "foo",
				},
			},
			fmt.Sprintf("targetRef must be to one of %v but was %s/%s",
				allowedTargetRefs, "wrong-group", gvk.KubernetesGateway.Kind), "",
		},
		{
			"target-ref-wrong-kind",
			&extensions.WasmPlugin{
				Url: "http://test.com/test",
				TargetRef: &api.PolicyTargetReference{
					Group: gvk.KubernetesGateway.Group,
					Kind:  "wrong-kind",
					Name:  "foo",
				},
			},
			fmt.Sprintf("targetRef must be to one of %v but was %s/%s",
				allowedTargetRefs, gvk.KubernetesGateway.Group, "wrong-kind"), "",
		},
		{
			"target-ref-and-selector-cannot-both-be-set",
			&extensions.WasmPlugin{
				Url: "http://test.com/test",
				TargetRef: &api.PolicyTargetReference{
					Group: gvk.KubernetesGateway.Group,
					Kind:  gvk.KubernetesGateway.Kind,
					Name:  "foo",
				},
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
			},
			"only one of targetRefs or workloadSelector can be set", "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warn, err := ValidateWasmPlugin(config.Config{
				Meta: config.Meta{
					Name:      someName,
					Namespace: someNamespace,
				},
				Spec: tt.in,
			})
			checkValidationMessage(t, warn, err, tt.warning, tt.out)
		})
	}
}

func TestValidateHTTPHeaderValue(t *testing.T) {
	cases := []struct {
		input    string
		expected error
	}{
		{
			input:    "foo",
			expected: nil,
		},
		{
			input:    "%HOSTNAME%",
			expected: nil,
		},
		{
			input:    "100%%",
			expected: nil,
		},
		{
			input:    "prefix %HOSTNAME% suffix",
			expected: nil,
		},
		{
			input:    "%DOWNSTREAM_PEER_CERT_V_END(%b %d %H:%M:%S %Y %Z)%",
			expected: nil,
		},
		{
			input: "%DYNAMIC_METADATA(com.test.my_filter)%",
		},
		{
			input:    "%START_TIME%%",
			expected: errors.New("header value configuration %START_TIME%% is invalid"),
		},
		{
			input:    "abc%123",
			expected: errors.New("header value configuration abc%123 is invalid"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := ValidateHTTPHeaderValue(tc.input)
			if tc.expected == nil {
				assert.NoError(t, got)
			} else {
				assert.Error(t, got)
			}
		})
	}
}
