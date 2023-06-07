package cli

import "testing"

func Test_handleNamespace(t *testing.T) {
	ns := handleNamespace("test", "default")
	if ns != "test" {
		t.Fatalf("Get the incorrect namespace: %q back", ns)
	}

	tests := []struct {
		description      string
		namespace        string
		defaultNamespace string
		wantNamespace    string
	}{
		{
			description:      "return test namespace",
			namespace:        "test",
			defaultNamespace: "default",
			wantNamespace:    "test",
		},
		{
			description:      "return default namespace",
			namespace:        "",
			defaultNamespace: "default",
			wantNamespace:    "default",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			gotNs := handleNamespace(tt.namespace, tt.defaultNamespace)
			if gotNs != tt.wantNamespace {
				t.Fatalf("unexpected namespace: wanted %v got %v", tt.wantNamespace, gotNs)
			}
		})
	}
}
