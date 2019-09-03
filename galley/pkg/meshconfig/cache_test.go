package meshconfig

import "testing"

func TestNewCache(t *testing.T) {
	fs, err := NewCacheFromFile("testdata/meshconfig.json")
	defer fs.Close()
	if err != nil {
		t.Fatal(err)
	}
	mesh := fs.Get()
	if mesh.MixerCheckServer != "bar:5555" {
		t.Fatalf("failed to read meshconfig, got %v", mesh)
	}
}
