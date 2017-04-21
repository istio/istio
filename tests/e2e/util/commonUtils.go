package util

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"path/filepath"

	"github.com/golang/glog"
)

const (
	TEST_SRCDIR = "TEST_SRCDIR"
	COM_GITHUB_ISTIO_ISTIO = "com_github_istio_istio"
)


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

func HttpDownload(dst string, src string) error {
	glog.Infof("Start downloading from %s to %s ...\n", src, dst)

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if resp, err := http.Get(src); err != nil {
		return err
	} else {
		defer resp.Body.Close()
		if _, err := io.Copy(out, resp.Body); err != nil {
			return err
		}
		glog.Info("Download successfully!")
		return nil
	}
}

func CopyFile(src, dst string) error {
	  var in, out *os.File
	  var err error
    in, err = os.Open(src)
    if err != nil {
        return err
    }
    defer in.Close()
    out, err = os.Create(dst)
    if err != nil {
        return err
    }
    defer out.Close()
    if _, err = io.Copy(out, in); err != nil {
        return err
    }
    err = out.Sync()
    return err
}

//Give "path from WORKSPACE", return absolute path at runtime
func GetTestRuntimePath(p string) string {
	//Using this part in local bazel run
	/*
	ex, err := os.Executable()
	if err != nil {
			glog.Warning("Cannot get runtime path")
	}
	return filepath.Join(path.Dir(ex), filepath.Join("go_default_test.runfiles/com_github_istio_istio", p))
	*/
	
	return filepath.Join(os.Getenv(TEST_SRCDIR), filepath.Join(COM_GITHUB_ISTIO_ISTIO, p))
}
