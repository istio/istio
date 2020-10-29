package crd

import (
	"testing"
)

func TestValidator(t *testing.T) {
	validator := NewIstioValidator(t)
	t.Run("valid", func(t *testing.T) {
		if err := validator.ValidateCustomResourceYAML(`
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
spec:
  mtls:
    mode: STRICT
`); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		if err := validator.ValidateCustomResourceYAML(`
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
spec:
  mtls:
    mode: BAD
`); err == nil {
			t.Fatal("expected error but got none")
		}
	})
}
