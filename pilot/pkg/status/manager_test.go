package status

import (
	"testing"

	"istio.io/istio/pkg/kube"
)

func TestManagerUpdate(t *testing.T) {
	client := kube.NewFakeClient()
	mgr := NewManager(client)
}
