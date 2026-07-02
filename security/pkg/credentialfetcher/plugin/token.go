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

package plugin

import (
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/security"
)

type KubernetesTokenPlugin struct {
	path      string
	mu        sync.Mutex
	lastMTime time.Time
	lastInode uint64
}

var _ security.CredFetcher = &KubernetesTokenPlugin{}

func CreateTokenPlugin(path string) *KubernetesTokenPlugin {
	return &KubernetesTokenPlugin{
		path: path,
	}
}

func (t *KubernetesTokenPlugin) GetPlatformCredential() (string, error) {
	if t.path == "" {
		return "", nil
	}

	info, err := os.Stat(t.path)
	if err != nil {
		log.Warnf("failed to stat token file %q: %v", t.path, err)
	} else {
		mtime := info.ModTime()
		inode := fileInode(info)
		t.mu.Lock()
		if t.lastMTime.IsZero() {
			t.lastMTime = mtime
			t.lastInode = inode
		} else if !mtime.Equal(t.lastMTime) || inode != t.lastInode {
			log.Infof("istio-token rotated: old_mtime=%v new_mtime=%v old_inode=%d new_inode=%d",
				t.lastMTime, mtime, t.lastInode, inode)
			t.lastMTime = mtime
			t.lastInode = inode
		}
		t.mu.Unlock()
	}

	tok, err := os.ReadFile(t.path)
	if err != nil {
		log.Warnf("failed to fetch token from file: %v", err)
		return "", nil
	}
	return strings.TrimSpace(string(tok)), nil
}

func (t *KubernetesTokenPlugin) GetIdentityProvider() string {
	return ""
}

func (t *KubernetesTokenPlugin) Stop() {
}

func fileInode(info os.FileInfo) uint64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return stat.Ino
	}
	return 0
}
