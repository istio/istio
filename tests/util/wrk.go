// Copyright 2017 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/golang/glog"
)

const (
	wrkDefaultBinaryPath = "/usr/local/bin/wrk"
	wrkDefaultURL        = "https://storage.googleapis.com/istio-tools/wrk/wrk-4.0.2"
	tmpPrefix            = "wrk"
)

var (
	wrkURL        = flag.String("wrk_url", wrkDefaultURL, "Download URL for wrk")
	wrkBinaryPath = flag.String("Wrk_bin_path", wrkDefaultBinaryPath, "Path of installed binary of wrk")
)

// Wrk is a wrapper around wrk.
type Wrk struct {
	BinaryPath string
	TmpDir     string
}

// LuaTemplate defines a Lua template or script.
type LuaTemplate struct {
	TemplatePath string
	Script       string
	Template     interface{}
}

// Generate creates the lua scripts in the .Script destination from the .Template.
func (l *LuaTemplate) Generate() error {
	if l.Script == "" {
		var err error
		l.Script, err = CreateTempfile(os.TempDir(), tmpPrefix, ".lua")
		if err != nil {
			return err
		}
	}
	return Fill(l.Script, l.TemplatePath, l.Template)
}

// ReadJSON creates a struct based on the input json path.
func ReadJSON(jsonPath string, i interface{}) error {
	data, err := ioutil.ReadFile(jsonPath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, i)
}

// Install installs wrk based on the URL provided if the binary is not already installed.
func (w *Wrk) Install() error {
	if _, err := os.Stat(*wrkBinaryPath); os.IsNotExist(err) {
		if w.TmpDir == "" {
			w.BinaryPath, err = CreateTempfile(os.TempDir(), tmpPrefix, ".bin")
			if err != nil {
				return err
			}
		} else {
			w.BinaryPath = filepath.Join(w.TmpDir, "wrk")
		}
		err = HTTPDownload(w.BinaryPath, *wrkURL)
		if err != nil {
			glog.Error("Failed to download wrk")
			return err
		}
		err = os.Chmod(w.BinaryPath, 0755) // #nosec
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		w.BinaryPath = *wrkBinaryPath
	}
	return nil
}

// Run runs a wrk command.
func (w *Wrk) Run(format string, args ...interface{}) error {
	format = w.BinaryPath + " " + format
	if _, err := Shell(format, args...); err != nil {
		glog.Errorf("wrk %s failed", args)
		return err
	}
	return nil
}
