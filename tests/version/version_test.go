package version

import (
	"encoding/json"
	"flag"
	"os/exec"
	"path"
	"strings"
	"testing"

	"istio.io/pkg/version"
)

var (
	binaries   *string
	releasedir *string
)

func init() {
	releasedir = flag.String("base-dir", "", "directory for binaries")
	binaries = flag.String("binaries", "", "space separated binaries to test")
	flag.Parse()

}

func TestVersion(t *testing.T) {
	binariesToTest := strings.Split(*binaries, " ")
	if len(binariesToTest) == 0 {
		t.Fatal("No binaries to test. Pass the --binaries flag.")
	}
	for _, b := range binariesToTest {
		cmd := path.Join(*releasedir, b)
		t.Run(b, func(t *testing.T) {
			out, err := exec.Command(cmd, "version", "-ojson").Output()
			if err != nil {
				t.Fatalf("Version failed with error: %v. Output: %v", err, string(out))
			}

			var resp version.Version
			if err := json.Unmarshal(out, &resp); err != nil {
				t.Fatalf("Failed to marshal to json: %v. Output: %v", err, string(out))
			}

			verInfo := resp.ClientVersion

			validateField(t, "Version", verInfo.Version)
			validateField(t, "GitRevision", verInfo.GitRevision)
			validateField(t, "User", verInfo.User)
			validateField(t, "Host", verInfo.Host)
			validateField(t, "GolangVersion", verInfo.GolangVersion)
			validateField(t, "DockerHub", verInfo.DockerHub)
			validateField(t, "BuildStatus", verInfo.BuildStatus)
			validateField(t, "GitTag", verInfo.GitTag)
		})
	}
}

// TODO we may be able to do more validation of fields here, but because it changes based on the environment this is not easy
// For now ensuring the fields even get populated is most important.
func validateField(t *testing.T, field, s string) {
	t.Helper()
	if s == "unknown" || s == "" {
		t.Errorf("Field %v was invalid. Got: %v.", field, s)
	}
}
