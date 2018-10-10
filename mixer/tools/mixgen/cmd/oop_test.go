package cmd

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"testing"
)

func TestOOPGenerator_Generate(t *testing.T) {
	templates := []string{
		"testdata/report/template.descriptor",
		"testdata/quota/template.descriptor",
		"testdata/check/template.descriptor",
	}
	adapterName := "TestAdapter"
	adapterPackage := "path/to/adapter/package"
	configPackge := "path/to/adapter/config/package"
	baselineDir := "testdata/oop"
	wantFiles := []string{
		"/cmd/server/nosession.go",
		"/cmd/Dockerfile",
		"/cmd/main.go",
		"/cmd/Makefile",
		"/helm/templates/deployment.yaml",
		"/helm/templates/service.yaml",
		"/helm/Chart.yaml",
		"/helm/values.yaml",
	}

	tmpDir := path.Join(os.TempDir(), "/oopScafolding")
	_ = os.MkdirAll(tmpDir, os.ModeDir|os.ModePerm)

	args := []string{"server", "--out_dir", tmpDir, "--adapter_name", adapterName,
		"--adapter_package", adapterPackage, "--config_package", configPackge}
	for _, t := range templates {
		args = append(args, "-t", t)
	}

	fmt.Println(strings.Join(args, " "))
	root := GetRootCmd(args,
		func(format string, a ...interface{}) {
		},
		func(format string, a ...interface{}) {
			t.Fatalf("want error 'nil'; got '%s'", fmt.Sprintf(format, a...))
		})

	_ = root.Execute()

	for _, want := range wantFiles {
		gfp := tmpDir + want
		gotBytes, err := ioutil.ReadFile(gfp)
		if err != nil {
			t.Fatalf("cannot find generated file %v", gfp)
		}

		bfp := baselineDir + want + ".golden"
		wantBytes, err := ioutil.ReadFile(bfp)
		if err != nil {
			t.Fatalf("cannot find baseline file %v", bfp)
		}

		if !bytes.Equal(wantBytes, gotBytes) {
			t.Errorf("generated file %v does not match baseline %v", bfp, gfp)
		}
	}
}
