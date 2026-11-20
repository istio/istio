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

package util

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileWatcherSeesMoveOut(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "10-primary.conflist")
	if err := os.WriteFile(primary, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	w, err := CreateFileWatcher(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// moving the primary out of the watched dir only raises Rename (MOVED_FROM)
	if err := os.Rename(primary, filepath.Join(t.TempDir(), "10-primary.conflist")); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Wait(ctx); err != nil {
		t.Fatalf("no event for primary moved out: %v", err)
	}
}
