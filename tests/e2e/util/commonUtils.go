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
	test_src_dir           = "TEST_SRCDIR"
	com_github_istio_istio = "com_github_istio_istio"
	version_file           = "istio.VERSION"
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

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	defer func() {
		if cerr := out.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()

	if resp, err := http.Get(src); err != nil {
		return err
	} else {
		defer func() {
			if cerr := resp.Body.Close(); err == nil && cerr != nil {
				err = cerr
			}
		}()
		if _, err := io.Copy(out, resp.Body); err != nil {
			return err
		}
		glog.Info("Download successfully!")
	}
	return nil
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
	if *manualRun {
		ex, err := os.Executable()
		if err != nil {
			glog.Warning("Cannot get runtime path")
		}
		return filepath.Join(path.Dir(ex), filepath.Join("go_default_test.runfiles/com_github_istio_istio", p))
	} else {
		return filepath.Join(os.Getenv(test_src_dir), filepath.Join(com_github_istio_istio, p))
	}
}

func PrintBlock(m string) {
	glog.Infof("\n\n=========================================\n%s\n=========================================\n\n", m)
}
