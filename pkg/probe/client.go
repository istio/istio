// Copyright 2018 Istio Authors
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

package probe

import (
	"fmt"
	"os"
	"time"
)

// PathExists checks if the specified path exists or not. This will be used
// by the k8s probe client commandline tool.
func PathExists(path string) error {
	_, err := os.Stat(path)
	return err
}

type Client interface {
	Check() error
}

type fileClient struct {
	path   string
	period time.Duration
}

func NewFileClient(path string, period time.Duration) Client {
	return &fileClient{
		path:   path,
		period: period,
	}
}

func (fc *fileClient) Check() error {
	stat, err := os.Stat(fc.path)
	if err != nil {
		return err
	}
	now := time.Now()
	if mtime := stat.ModTime(); now.Sub(mtime) > fc.period*2 {
		return fmt.Errorf("file %s is too old (last modified time %v, now %v, should be within %v)", fc.path, mtime, now, fc.period)
	}
	return nil
}
