package yml

import (
	"testing"
)

func TestApplyNamespace(t *testing.T) {
	cases := []struct {
		name      string
		yml       string
		namespace string
		expected  string
	}{
		{
			"no namespace",
			`metadata:
  name: foo`,
			"default",
			`metadata:
  name: foo
  namespace: default`,
		},
		{
			"empty namespace",
			`metadata:
  name: foo
  namespace: ""`,
			"default",
			`metadata:
  name: foo
  namespace: default`,
		},
		{
			"existing namespace",
			`metadata:
  name: foo
  namespace: bar`,
			"default",
			`metadata:
  name: foo
  namespace: bar`,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyNamespace(tt.yml, tt.namespace)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.expected {
				t.Fatalf("expected '%s', got '%s'", tt.expected, got)
			}
		})
	}
}
