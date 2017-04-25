package util

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/golang/glog"
)

const (
	testSrcDir = "TEST_SRCDIR"
	pathPrefix = "com_github_istio_istio"
)

var (
	manualRun = flag.Bool("manual_run", false, "If runned by bazel run")
)

// Shell run command on shell and get back output and error if get one
func Shell(command string) (string, error) {
	glog.Info(command)
	parts := strings.Split(command, " ")
	c := exec.Command(parts[0], parts[1:]...)
	bytes, err := c.CombinedOutput()
	if err != nil {
		glog.V(2).Info(string(bytes))
		return "", fmt.Errorf("command failed: %q %v", string(bytes), err)
	}
	return string(bytes), nil
}

// HTTPDownload download from src(url) and store into dst(local file)
func HTTPDownload(dst string, src string) error {
	glog.Infof("Start downloading from %s to %s ...\n", src, dst)
	var err error
	var out *os.File
	var resp *http.Response
	out, err = os.Create(dst)
	if err != nil {
		return err
	}

	defer func() {
		if cerr := out.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()

	resp, err = http.Get(src)
	defer func() {
		if cerr := resp.Body.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()
	if err == nil {
		if _, err = io.Copy(out, resp.Body); err != nil {
			return err
		}
		glog.Info("Download successfully!")
	}
	return err
}

// CopyFile create a new file to src based on dst
func CopyFile(src, dst string) error {
	var in, out *os.File
	var err error
	in, err = os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := in.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()
	out, err = os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	err = out.Sync()
	return err
}

// GetTestRuntimePath give "path from WORKSPACE", return absolute path at runtime
func GetTestRuntimePath(p string) string {
	var res string
	if *manualRun {
		ex, err := os.Executable()
		if err != nil {
			glog.Warning("Cannot get runtime path")
		}
		res = filepath.Join(path.Dir(ex), filepath.Join(filepath.Join("go_default_test.runfiles", pathPrefix), p))
	} else {
		res = filepath.Join(os.Getenv(testSrcDir), filepath.Join(pathPrefix, p))
	}
	return res
}

// PrintBlock print log in a clearer way
func PrintBlock(m string) {
	glog.Infof("\n\n=========================================\n%s\n=========================================\n\n", m)
}
