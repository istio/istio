//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package envoy_test

import (
	"bufio"
	"bytes"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"

	"istio.io/istio/pkg/test/envoy"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	adminYAML = `
admin:
  access_log_path: "/dev/null"
  address:
    socket_address:
      address: 127.0.0.1
      port_value: {{.AdminPort}}
`
)

func TestAddressAlreadyInUse(t *testing.T) {
	// Bind to an address.
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	// Get the bound port.
	port := l.Addr().(*net.TCPAddr).Port

	tmpDir := createTempDir(t)
	defer deleteDir(tmpDir)

	// Create the bootstrap file using the bound port.
	bootstrapFile := generateBootstrapFile(t, tmpDir, adminYAML, map[string]interface{}{
		"AdminPort": port,
	})

	t.Run("STDERR", func(t *testing.T) {
		e := &envoy.Envoy{
			YamlFile: bootstrapFile,
			// Verify even with logs off, that we generate the error.
			LogLevel: envoy.LogLevelOff,
		}
		expectAddressInUse(t, e)
	})

	t.Run("LogFile", func(t *testing.T) {
		logFile := createTempfile(t, tmpDir, "logfile", "txt")
		e := envoy.Envoy{
			YamlFile:    bootstrapFile,
			LogFilePath: logFile,
			// Verify even with logs off, that we generate the error.
			LogLevel: envoy.LogLevelOff,
		}
		if err := e.Start(); err == nil {
			t.Fatal("expected startup error")
		}
	})

}

func TestSuccessfulStartup(t *testing.T) {
	tmpDir := createTempDir(t)
	defer deleteDir(tmpDir)

	// Bind to an address.
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}

	// Get the bound port.
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	// Create the bootstrap file using the bound port.
	bootstrapFile := generateBootstrapFile(t, tmpDir, adminYAML, map[string]interface{}{
		"AdminPort": port,
	})

	_, err = retry.Do(func() (result interface{}, completed bool, err error) {
		e := &envoy.Envoy{
			YamlFile: bootstrapFile,
			LogLevel: envoy.LogLevelInfo,
		}
		if err := e.Start(); err != nil {
			return nil, false, err
		}
		_ = e.Stop()
		return nil, true, nil
	}, retry.Delay(1*time.Second), retry.Timeout(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
}

func expectAddressInUse(t *testing.T, e *envoy.Envoy) {
	if err := e.Start(); err == nil {
		t.Fatal("expected startup error")
	} else if !strings.Contains(err.Error(), "Address already in use") {
		t.Fatalf("unexpected error: %v", err)
	}
}
func createTempDir(t *testing.T) string {
	t.Helper()
	tmpDir, err := ioutil.TempDir(os.TempDir(), "envoy_test")
	if err != nil {
		t.Fatal(err)
	}
	return tmpDir
}

func deleteDir(dir string) {
	_ = os.Remove(dir)
}

func createTempfile(t *testing.T, tmpDir, prefix, suffix string) string {
	t.Helper()
	f, err := ioutil.TempFile(tmpDir, prefix)
	if err != nil {
		t.Fatal(err)
	}
	var tmpName string
	if tmpName, err = filepath.Abs(f.Name()); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(tmpName); err != nil {
		t.Fatal(err)
	}
	return tmpName + suffix
}

func generateBootstrapFile(t *testing.T, tmpDir string, yamlTemplate string, values map[string]interface{}) string {
	t.Helper()

	// Create an output file to hold the generated configuration.
	outFile := createTempfile(t, tmpDir, "envoy_config", ".yaml")

	// Create a template object.
	templateObject := template.New("envoy_test")
	_, err := templateObject.Parse(yamlTemplate)
	if err != nil {
		t.Fatal(err)
	}

	// Apply the template with the current configuration
	var filled bytes.Buffer
	w := bufio.NewWriter(&filled)
	if err := templateObject.Execute(w, values); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}

	// Write the content of the file.
	configBytes := filled.Bytes()
	if err := ioutil.WriteFile(outFile, configBytes, 0644); err != nil {
		t.Fatal(err)
	}
	return outFile
}
