package v1alpha3

import "testing"

func TestGetClusterNameFromURL(t *testing.T) {
	cluster, err := thritRLSClusterNameFromAuthority("")
	if cluster != "" {
		t.Fatalf("should return empty url on error (got %v)", cluster)
	}
	cluster, err = thritRLSClusterNameFromAuthority("host.com:80")
	if err != nil {
		t.Fatal("host without port should not cause error")
	}
	if cluster != "outbound|80||host.com" {
		t.Fatalf("Should return correct cluster name (got %v)", cluster)
	}
}
