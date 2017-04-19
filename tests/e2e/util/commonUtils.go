package util

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/golang/glog"
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

func WebDownload(dst string, src string) error {
	glog.Infof("Start downloading from %s to %s ...\n", src, dst)
	out, eCreate := os.Create(dst)
	if eCreate != nil {
		return eCreate
	}
	defer out.Close()

	resp, eGet := http.Get(src)
	if eGet != nil {
		return eGet
	}
	defer resp.Body.Close()
	if _, e := io.Copy(out, resp.Body); e != nil {
		return e
	}
	glog.Info("Download successfully!")
	return nil
}
