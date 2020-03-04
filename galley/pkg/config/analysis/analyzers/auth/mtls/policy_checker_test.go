// Copyright 2019 Istio Authors
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

package mtls

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"

	"istio.io/istio/pkg/config/resource"

	"istio.io/api/authentication/v1alpha1"
)

func TestMTLSPolicyChecker_singleResource(t *testing.T) {
	type PolicyResource struct {
		namespace resource.Namespace
		policy    string
	}

	type PortNameMapping struct {
		fqdn   string
		name   string
		number uint32
	}

	tests := map[string]struct {
		meshPolicy       string
		portNameMappings []PortNameMapping
		policy           PolicyResource
		service          TargetService
		want             Mode
	}{
		"no policies means no strict mtls": {
			// Note no policies specified
			service: NewTargetServiceWithPortNumber("foobar.my-namespace.svc.cluster.local", 8080),
			want:    ModePlaintext,
		},
		"service specific policy": {
			policy: PolicyResource{
				namespace: "my-namespace",
				policy: `
targets:
- name: foobar
  ports:
  - number: 8080
peers:
- mtls:
`,
			},
			service: NewTargetServiceWithPortNumber("foobar.my-namespace.svc.cluster.local", 8080),
			want:    ModeStrict,
		},
		"service specific policy uses only the first mtls configuration found": {
			policy: PolicyResource{
				namespace: "my-namespace",
				policy: `
targets:
- name: foobar
  ports:
  - number: 8080
peers:
# Oops, we specified mtls twice!
- mtls:
    mode: PERMISSIVE
- mtls:
`,
			},
			service: NewTargetServiceWithPortNumber("foobar.my-namespace.svc.cluster.local", 8080),
			want:    ModePermissive,
		},
		"service specific policy using port name": {
			portNameMappings: []PortNameMapping{
				{
					fqdn:   "foobar.my-namespace.svc.cluster.local",
					name:   "https",
					number: 8080,
				},
			},
			policy: PolicyResource{

				namespace: "my-namespace",
				policy: `
targets:
- name: foobar
  ports:
  - name: https
peers:
- mtls:
`,
			},
			service: NewTargetServiceWithPortNumber("foobar.my-namespace.svc.cluster.local", 8080),
			want:    ModeStrict,
		},
		"non-matching host service specific policy": {
			policy: PolicyResource{

				namespace: "my-namespace",
				policy: `
targets:
- name: baz
  ports:
  - number: 8080
peers:
- mtls:
`,
			},
			service: NewTargetServiceWithPortNumber("foobar.my-namespace.svc.cluster.local", 8080),
			want:    ModePlaintext,
		},
		"non-matching namespace service specific policy": {
			policy: PolicyResource{
				namespace: "my-other-namespace",
				policy: `
targets:
- name: foobar
  ports:
  - number: 8080
peers:
- mtls:
`,
			},
			service: NewTargetServiceWithPortNumber("foobar.my-namespace.svc.cluster.local", 8080),
			want:    ModePlaintext,
		},
		"policy matches service but is not strict": {
			policy: PolicyResource{

				namespace: "my-namespace",
				policy: `
targets:
- name: foobar
  ports:
  - number: 8080
peers:
- mtls:
    mode: PERMISSIVE
`,
			},
			service: NewTargetServiceWithPortNumber("foobar.my-namespace.svc.cluster.local", 8080),
			want:    ModePermissive,
		},
		"policy matches service but uses deprecated field": {
			policy: PolicyResource{

				namespace: "my-namespace",
				policy: `
targets:
- name: foobar
  ports:
  - number: 8080
peers:
- mtls:
    allowTls: true
    mode: STRICT
`,
			},
			service: NewTargetServiceWithPortNumber("foobar.my-namespace.svc.cluster.local", 8080),
			want:    ModePermissive,
		},
		"policy matches service but peer is optional": {
			policy: PolicyResource{

				namespace: "my-namespace",
				policy: `
targets:
- name: foobar
  ports:
  - number: 8080
peers:
- mtls:
    mode: STRICT
peerIsOptional: true
`,
			},
			service: NewTargetServiceWithPortNumber("foobar.my-namespace.svc.cluster.local", 8080),
			want:    ModePermissive,
		},
		"policy matches service but does not use mtls": {
			policy: PolicyResource{

				namespace: "my-namespace",
				policy: `
targets:
- name: foobar
  ports:
  - number: 8080
peers:
- jwt: # undocumented setting?
`,
			},
			service: NewTargetServiceWithPortNumber("foobar.my-namespace.svc.cluster.local", 8080),
			want:    ModePlaintext,
		},
		"policy matches every port on service": {
			policy: PolicyResource{

				namespace: "my-namespace",
				policy: `
targets:
- name: foobar
peers:
- mtls:
`,
			},
			service: NewTargetServiceWithPortNumber("foobar.my-namespace.svc.cluster.local", 8080),
			want:    ModeStrict,
		},
		"policy matches every service in namespace": {
			policy: PolicyResource{
				namespace: "my-namespace",
				policy: `
peers:
- mtls:
`,
			},
			service: NewTargetServiceWithPortNumber("foobar.my-namespace.svc.cluster.local", 8080),
			want:    ModeStrict,
		},
		"policy matches entire mesh": {
			meshPolicy: `
peers:
- mtls:
`,
			service: NewTargetServiceWithPortNumber("foobar.my-namespace.svc.cluster.local", 8080),
			want:    ModeStrict,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			fqdnToPortNameToNumber := make(map[string]map[string]uint32)
			for _, portMapping := range tc.portNameMappings {
				if _, ok := fqdnToPortNameToNumber[portMapping.fqdn]; !ok {
					fqdnToPortNameToNumber[portMapping.fqdn] = make(map[string]uint32)
				}

				fqdnToPortNameToNumber[portMapping.fqdn][portMapping.name] = portMapping.number
			}

			pc := NewPolicyChecker(fqdnToPortNameToNumber)

			if tc.meshPolicy != "" {
				meshpb, err := yAMLToPolicy(tc.meshPolicy)
				if err != nil {
					t.Fatalf("expected: %v, got error when parsing yaml: %v", tc.want, err)
				}
				r := resource.Instance{Metadata: resource.Metadata{FullName: resource.NewFullName("", "default")}}
				if err := pc.AddMeshPolicy(&r, meshpb); err != nil {
					t.Fatal(err)
				}
			}

			if tc.policy.policy != "" {
				pb, err := yAMLToPolicy(tc.policy.policy)
				if err != nil {
					t.Fatalf("expected: %v, got error when parsing yaml: %v", tc.want, err)
				}
				r := resource.Instance{Metadata: resource.Metadata{FullName: resource.NewFullName(tc.policy.namespace, "somePolicy")}}
				err = pc.AddPolicy(&r, pb)
				if err != nil {
					t.Fatalf("expected: %v, got error when adding policy: %v", tc.want, err)
				}
			}

			mr, err := pc.IsServiceMTLSEnforced(tc.service)
			if err != nil {
				t.Fatalf("expected: %v, got error: %v", tc.want, err)
			}
			got := mr.MTLSMode
			if got != tc.want {
				t.Fatalf("expected: %v, got: %v", tc.want, got)
			}
		})
	}
}

func TestMTLSPolicyChecker_multipleResources(t *testing.T) {
	type PolicyResource struct {
		namespace resource.Namespace
		policy    string
	}

	type PortNameMapping struct {
		fqdn   string
		name   string
		number uint32
	}

	tests := map[string]struct {
		meshPolicy       string
		portNameMappings []PortNameMapping
		policies         []PolicyResource
		service          TargetService
		want             Mode
	}{
		"namespace policy overrides mesh policy": {
			meshPolicy: `
peers:
- mtls:
`,
			policies: []PolicyResource{
				{
					namespace: "my-namespace",
					policy: `
targets:
- name: foobar
  ports:
  - number: 8080
peers:
- mtls:
    mode: PERMISSIVE
`,
				},
			},
			service: NewTargetServiceWithPortNumber("foobar.my-namespace.svc.cluster.local", 8080),
			want:    ModePermissive,
		},
		"service policy overrides namespace policy": {
			policies: []PolicyResource{
				{
					namespace: "my-namespace",
					policy: `
peers:
- mtls:
    mode: STRICT
`,
				},
				{
					namespace: "my-namespace",
					policy: `
targets:
- name: foobar
  ports:
  - number: 8080
peers:
- mtls:
    mode: PERMISSIVE
`,
				},
			},
			service: NewTargetServiceWithPortNumber("foobar.my-namespace.svc.cluster.local", 8080),
			want:    ModePermissive,
		},
		"port-specific policy overrides service-level policy": {
			policies: []PolicyResource{
				{
					namespace: "my-namespace",
					policy: `
targets:
- name: foobar
peers:
- mtls:
    mode: STRICT
`,
				},
				{
					namespace: "my-namespace",
					policy: `
targets:
- name: foobar
  ports:
  - number: 8080
peers:
- mtls:
    mode: PERMISSIVE
`,
				},
			},
			service: NewTargetServiceWithPortNumber("foobar.my-namespace.svc.cluster.local", 8080),
			want:    ModePermissive,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			fqdnToPortNameToNumber := make(map[string]map[string]uint32)
			for _, portMapping := range tc.portNameMappings {
				if _, ok := fqdnToPortNameToNumber[portMapping.fqdn]; !ok {
					fqdnToPortNameToNumber[portMapping.fqdn] = make(map[string]uint32)
				}

				fqdnToPortNameToNumber[portMapping.fqdn][portMapping.name] = portMapping.number
			}

			pc := NewPolicyChecker(fqdnToPortNameToNumber)
			// Add mesh policy, if it exists.
			meshpb, err := yAMLToPolicy(tc.meshPolicy)
			if err != nil {
				t.Fatalf("expected: %v, got error when parsing yaml: %v", tc.want, err)
			}
			r := resource.Instance{Metadata: resource.Metadata{FullName: resource.NewFullName("", "default")}}
			if err := pc.AddMeshPolicy(&r, meshpb); err != nil {
				t.Fatal(err)
			}

			// Add in all other policies
			for _, p := range tc.policies {
				pb, err := yAMLToPolicy(p.policy)
				if err != nil {
					t.Fatalf("expected: %v, got error when parsing yaml: %v", tc.want, err)
				}
				r := resource.Instance{Metadata: resource.Metadata{FullName: resource.NewFullName(p.namespace, "somePolicy")}}
				err = pc.AddPolicy(&r, pb)
				if err != nil {
					t.Fatalf("expected: %v, got error when adding policy: %v", tc.want, err)
				}
			}

			mr, err := pc.IsServiceMTLSEnforced(tc.service)
			if err != nil {
				t.Fatalf("expected: %v, got error: %v", tc.want, err)
			}
			got := mr.MTLSMode
			if got != tc.want {
				t.Fatalf("expected: %v, got: %v", tc.want, got)
			}
		})
	}
}

func yAMLToPolicy(yml string) (*v1alpha1.Policy, error) {
	js, err := yaml.YAMLToJSON([]byte(yml))
	if err != nil {
		return nil, fmt.Errorf("error translating yaml to json: %w", err)
	}
	var pb v1alpha1.Policy
	err = jsonpb.Unmarshal(bytes.NewReader(js), &pb)
	if err != nil {
		return nil, fmt.Errorf("error translating json to pb for json: %s, error: %w", string(js), err)
	}

	return &pb, nil
}
