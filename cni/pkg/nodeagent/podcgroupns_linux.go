// Copyright Istio Authors
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

package nodeagent

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strconv"
	"syscall"
)

func GetStat(fi fs.FileInfo) (*syscall.Stat_t, error) {
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		return stat, nil
	}
	return nil, fmt.Errorf("unable to stat %s", fi.Name())
}

// Gets the `starttime` field from `/proc/<pid>/stat`.
// Note that this value is ticks since boot, and is not wallclock time
func GetStarttime(proc fs.FS, pidDir fs.DirEntry) (uint64, error) {
	statFile, err := proc.Open(path.Join(pidDir.Name(), "stat"))
	if err != nil {
		return 0, err
	}
	defer statFile.Close()

	data, err := io.ReadAll(statFile)
	if err != nil {
		return 0, err
	}

	lastParen := bytes.LastIndex(data, []byte(")"))
	if lastParen == -1 {
		return 0, fmt.Errorf("invalid stat format")
	}

	fields := bytes.Fields(data[lastParen+1:])
	if len(fields) < 20 {
		return 0, fmt.Errorf("not enough fields in stat")
	}

	return strconv.ParseUint(string(fields[19]), 10, 64)
}
