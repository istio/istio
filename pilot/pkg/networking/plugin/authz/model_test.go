package authz

import "testing"

func TestPermission_Match(t *testing.T) {
	cases := []struct {
		name    string
		service *serviceMetadata
		perm    *Permission
		want    bool
	}{
		{
			name:    "empty permission",
			service: &serviceMetadata{},
			want:    true,
		},
		{
			name: "service.name not matched",
			service: &serviceMetadata{
				name:       "product.default",
				attributes: map[string]string{"destination.name": "s2"},
			},
			perm: &Permission{
				Services: []string{"review.default"},
				Constraints: []KeyValues{
					{
						"destination.name": []string{"s1", "s2"},
					},
				},
			},
			want: false,
		},
		{
			name: "destination.name not matched",
			service: &serviceMetadata{
				name:       "product.default",
				attributes: map[string]string{"destination.name": "s3"},
			},
			perm: &Permission{
				Services: []string{"product.default"},
				Constraints: []KeyValues{
					{
						"destination.name": []string{"s1", "s2"},
					},
				},
			},
			want: false,
		},
		{
			name: "destination.labels not matched",
			service: &serviceMetadata{
				name:   "product.default",
				labels: map[string]string{"token": "t3"},
			},
			perm: &Permission{
				Services: []string{
					"product.default",
				},
				Constraints: []KeyValues{
					{
						"destination.labels[token]": []string{"t1", "t2"},
					},
				},
			},
			want: false,
		},
		{
			name: "all matched",
			service: &serviceMetadata{
				name: "product.default",
				attributes: map[string]string{
					"destination.name":      "s2",
					"destination.namespace": "ns2",
					"destination.user":      "sa2",
					"other":                 "other",
				},
				labels: map[string]string{"token": "t2"},
			},
			perm: &Permission{
				Services: []string{"product.default"},
				Constraints: []KeyValues{
					{
						"destination.name": []string{"s1", "s2"},
					},
					{
						"destination.namespace": []string{"ns1", "ns2"},
					},
					{
						"destination.user": []string{"sa1", "sa2"},
					},
					{
						"destination.labels[token]": []string{"t1", "t2"},
					},
					{
						"request.headers[user-agent]": []string{"x1", "x2"},
					},
				},
			},
			want: true,
		},
	}

	for _, tc := range cases {
		if actual := tc.perm.Match(tc.service); actual != tc.want {
			t.Errorf("%s: want %v but got %v", tc.name, tc.want, actual)
		}
	}
}

func TestPermission_Validate(t *testing.T) {
	testCases := []struct {
		name string
		perm *Permission
		want bool
	}{
		{
			name: "nil permission",
			want: true,
		},
		{
			name: "permission with path",
			perm: &Permission{
				Paths: []string{"/"},
			},
		},
		{
			name: "permission with method",
			perm: &Permission{
				Methods: []string{"GET"},
			},
		},
		{
			name: "permission with unsupported constraint",
			perm: &Permission{
				Constraints: []KeyValues{
					{
						attrDestIP: []string{"1.2.3.4"},
					},
					{
						attrRequestHeader: []string{"TOKEN"},
					},
				},
			},
		},
		{
			name: "good permission",
			perm: &Permission{
				Constraints: []KeyValues{
					{
						attrDestIP: []string{"1.2.3.4"},
					},
					{
						attrDestPort: []string{"80"},
					},
				},
			},
			want: true,
		},
	}
	for _, tc := range testCases {
		err := tc.perm.ValidateForTCP(true)
		got := err == nil
		if tc.want != got {
			t.Errorf("%s: want %v bot got: %s", tc.name, tc.want, err)
		}
	}
}

func TestPrincipal_Validate(t *testing.T) {
	testCases := []struct {
		name      string
		principal *Principal
		want      bool
	}{
		{
			name: "empty principal",
			want: true,
		},
		{
			name: "principal with group",
			principal: &Principal{
				Group: "group",
			},
		},
		{
			name: "principal with unsupported property",
			principal: &Principal{
				Properties: []KeyValues{
					{
						attrRequestPresenter: []string{"ns"},
					},
				},
			},
		},
		{
			name: "good principal",
			principal: &Principal{
				User: "user",
				Properties: []KeyValues{
					{
						attrSrcNamespace: []string{"ns"},
					},
					{
						attrSrcPrincipal: []string{"p"},
					},
				},
			},
			want: true,
		},
	}
	for _, tc := range testCases {
		err := tc.principal.ValidateForTCP(true)
		got := err == nil
		if tc.want != got {
			t.Errorf("%s: want %v bot got: %s", tc.name, tc.want, err)
		}
	}
}
