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

package nftables

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fakeProc describes a single synthetic /proc/<pid> entry to write to a fake procfs root.
type fakeProc struct {
	pid     int
	comm    string
	cmdline []string
	uid     uint64
}

// writeFakeProc creates the subset of /proc/<pid>/* files that getKubeletUIDFromPath reads.
func writeFakeProc(t *testing.T, procRoot string, p fakeProc) {
	t.Helper()

	pidDir := filepath.Join(procRoot, strconv.Itoa(p.pid))
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatalf("failed to create fake proc dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(pidDir, "comm"), []byte(p.comm+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write comm: %v", err)
	}

	cmdline := strings.Join(p.cmdline, "\x00")
	if len(p.cmdline) > 0 {
		cmdline += "\x00"
	}
	if err := os.WriteFile(filepath.Join(pidDir, "cmdline"), []byte(cmdline), 0o644); err != nil {
		t.Fatalf("failed to write cmdline: %v", err)
	}

	status := fmt.Sprintf("Uid:\t%d\t%d\t%d\t%d\n", p.uid, p.uid, p.uid, p.uid)
	if err := os.WriteFile(filepath.Join(pidDir, "status"), []byte(status), 0o644); err != nil {
		t.Fatalf("failed to write status: %v", err)
	}
}

func TestGetKubeletUIDFromPath(t *testing.T) {
	tests := []struct {
		name    string
		procs   []fakeProc
		wantUID string
		wantErr bool
	}{
		{
			name: "kubelet",
			procs: []fakeProc{
				{pid: 100, comm: "kubelet", cmdline: []string{"/usr/bin/kubelet", "--config=/var/lib/kubelet/config.yaml"}, uid: 1001},
			},
			wantUID: "1001",
		},
		{
			name: "kubelite",
			procs: []fakeProc{
				{pid: 100, comm: "kubelite", cmdline: []string{"/snap/microk8s/current/bin/kubelite", "--kubelet"}, uid: 1002},
			},
			wantUID: "1002",
		},
		{
			name: "k3s",
			procs: []fakeProc{
				// A default k3s invocation has no "kubelet" substring anywhere in argv.
				{pid: 100, comm: "k3s", cmdline: []string{"k3s", "server"}, uid: 1003},
			},
			wantUID: "1003",
		},
		{
			name: "no_match",
			procs: []fakeProc{
				{pid: 100, comm: "bash", cmdline: []string{"/bin/bash"}, uid: 1000},
			},
			wantErr: true,
		},
		{
			name: "multiple_processes",
			procs: []fakeProc{
				{pid: 50, comm: "sshd", cmdline: []string{"/usr/sbin/sshd", "-D"}, uid: 0},
				{pid: 75, comm: "bash", cmdline: []string{"/bin/bash"}, uid: 1000},
				{pid: 100, comm: "kubelet", cmdline: []string{"/usr/bin/kubelet"}, uid: 1004},
			},
			wantUID: "1004",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			procRoot := t.TempDir()
			for _, p := range tt.procs {
				writeFakeProc(t, procRoot, p)
			}

			uid, err := getKubeletUIDFromPath(procRoot)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got uid %q", uid)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if uid != tt.wantUID {
				t.Fatalf("got uid %q, want %q", uid, tt.wantUID)
			}
		})
	}
}
