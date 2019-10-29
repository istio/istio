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
	"testing"

	"github.com/ghodss/yaml"

	"github.com/golang/protobuf/jsonpb"

	"istio.io/istio/security/proto/authentication/v1alpha1"
)

func TestMTLSPolicyChecker_singleResource(t *testing.T) {
	type PolicyResource struct {
		namespace string
		policy    string
	}

	tests := map[string]struct {
		meshPolicy string
		policy     PolicyResource
		service    TargetService
		want       bool
	}{
		"no policies means no strict mtls": {
			// Note no policies specified
			service: NewTargetServiceWithPortNumber("foobar.my-namespace.svc.cluster.local", 8080),
			want:    false,
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
			want:    true,
		},
		"service specific policy using port name": {
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
			service: NewTargetServiceWithPortName("foobar.my-namespace.svc.cluster.local", "https"),
			want:    true,
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
			want:    false,
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
			want:    false,
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
			want:    false,
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
			want:    false,
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
			want:    false,
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
			want:    false,
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
			want:    true,
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
			want:    true,
		},
		"policy matches entire mesh": {
			meshPolicy: `
peers:
- mtls:
`,
			service: NewTargetServiceWithPortNumber("foobar.my-namespace.svc.cluster.local", 8080),
			want:    true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			pc := NewPolicyChecker()

			if tc.meshPolicy != "" {
				meshpb, err := yAMLToPolicy(tc.meshPolicy)
				if err != nil {
					t.Fatalf("expected: %v, got error when parsing yaml: %v", tc.want, err)
				}
				pc.AddMeshPolicy(meshpb)
			}

			if tc.policy.policy != "" {
				pb, err := yAMLToPolicy(tc.policy.policy)
				if err != nil {
					t.Fatalf("expected: %v, got error when parsing yaml: %v", tc.want, err)
				}
				pc.AddPolicy(tc.policy.namespace, pb)
			}

			got, err := pc.IsServiceMTLSEnforced(tc.service)
			if err != nil {
				t.Fatalf("expected: %v, got error: %v", tc.want, err)
			}
			if got != tc.want {
				t.Fatalf("expected: %v, got: %v", tc.want, got)
			}
		})
	}
}

func TestMTLSPolicyChecker_multipleResources(t *testing.T) {
	type PolicyResource struct {
		namespace string
		policy    string
	}

	tests := map[string]struct {
		meshPolicy string
		policies   []PolicyResource
		service    TargetService
		want       bool
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
			want:    false,
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
			want:    false,
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
			want:    false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			pc := NewPolicyChecker()
			// Add mesh policy, if it exists.
			meshpb, err := yAMLToPolicy(tc.meshPolicy)
			if err != nil {
				t.Fatalf("expected: %v, got error when parsing yaml: %v", tc.want, err)
			}
			pc.AddMeshPolicy(meshpb)

			// Add in all other policies
			for _, p := range tc.policies {
				pb, err := yAMLToPolicy(p.policy)
				if err != nil {
					t.Fatalf("expected: %v, got error when parsing yaml: %v", tc.want, err)
				}
				pc.AddPolicy(p.namespace, pb)
			}

			got, err := pc.IsServiceMTLSEnforced(tc.service)
			if err != nil {
				t.Fatalf("expected: %v, got error: %v", tc.want, err)
			}
			if got != tc.want {
				t.Fatalf("expected: %v, got: %v", tc.want, got)
			}
		})
	}
}

func yAMLToPolicy(yml string) (*v1alpha1.Policy, error) {
	js, err := yaml.YAMLToJSON([]byte(yml))
	if err != nil {
		return nil, err
	}
	var pb v1alpha1.Policy
	err = jsonpb.Unmarshal(bytes.NewReader(js), &pb)
	if err != nil {
		return nil, err
	}

	return &pb, nil
}
