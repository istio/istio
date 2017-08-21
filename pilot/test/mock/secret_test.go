package mock

import (
	"fmt"
	"testing"

	"istio.io/pilot/model"
)

func TestSecret(t *testing.T) {
	tls := &model.TLSSecret{
		Certificate: []byte("abcdef"),
		PrivateKey:  []byte("ghijkl"),
	}
	ns := "default"
	uri := fmt.Sprintf("%v.%v", ns, "host1")

	s := SecretRegistry{uri: tls}
	if secret, err := s.GetTLSSecret(uri); err != nil {
		t.Fatalf("GetTLSSecret(%q) -> %q", uri, err)
	} else if secret == nil {
		t.Fatalf("GetTLSSecret(%q) -> not found", uri)
	}
}
