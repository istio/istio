package translate

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"istio.io/istio/operator/pkg/util"
)

var (
	testDataDir string
	repoRootDir string
)

func init() {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	repoRootDir = filepath.Join(wd, "../..")
	testDataDir = filepath.Join(wd, "testdata/icp-iop")
}

func TestTranslateYAMLTree(t *testing.T) {
	tests := []struct {
		desc string
	}{
		{
			desc: "default",
		},
		{
			desc: "gateways",
		},
		{
			desc: "addons",
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			inPath := filepath.Join(testDataDir, "input", tt.desc+".yaml")
			outPath := filepath.Join(testDataDir, "output", tt.desc+".yaml")

			translations, err := ReadTranslations(filepath.Join(repoRootDir, "data/translateConfig/translate-ICP-IOP-1.5.yaml"))
			if err != nil {
				t.Fatal(err)
			}

			icp, err := readFile(inPath)
			if err != nil {
				t.Fatal(err)
			}

			got, err := TranslateICPToIOP(icp, translations)
			if err != nil {
				t.Fatal(err)
			}

			if refreshGoldenFiles() {
				t.Logf("Refreshing golden file for %s", outPath)
				if err := ioutil.WriteFile(outPath, []byte(got), 0644); err != nil {
					t.Error(err)
				}
			}

			want, err := readFile(outPath)
			if err != nil {
				t.Fatal(err)
			}

			if !util.IsYAMLEqual(got, want) {
				t.Errorf("%s got:\n%s\n\nwant:\n%s\nDiff:\n%s\n", tt.desc, got, want, util.YAMLDiff(got, want))
			}
		})
	}
}

func refreshGoldenFiles() bool {
	return os.Getenv("UPDATE_GOLDENS") == "true"
}

func readFile(path string) (string, error) {
	b, err := ioutil.ReadFile(path)
	return string(b), err
}
