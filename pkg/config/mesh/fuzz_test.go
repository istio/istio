package mesh

import "testing"

func FuzzValidateMeshConfig(f *testing.F) {
	f.Fuzz(func(t *testing.T, data string) {
		_, _ = ApplyMeshConfigDefaults(data)
	})
}
