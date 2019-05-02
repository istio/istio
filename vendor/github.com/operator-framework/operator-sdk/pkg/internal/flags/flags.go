// Copyright 2019 The Operator-SDK Authors
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

package flags

import (
	"strings"
	"time"

	"github.com/spf13/pflag"
)

// WatchFlags provides flag for configuration of a controller's reconcile period and for a
// watches.yaml file, which is used to configure dynamic operators (e.g. Ansible and Helm).
type WatchFlags struct {
	ReconcilePeriod time.Duration
	WatchesFile     string
}

// AddTo - Add the reconcile period and watches file flags to the the flagset
// helpTextPrefix will allow you add a prefix to default help text. Joined by a space.
func (f *WatchFlags) AddTo(flagSet *pflag.FlagSet, helpTextPrefix ...string) {
	flagSet.DurationVar(&f.ReconcilePeriod,
		"reconcile-period",
		time.Minute,
		strings.Join(append(helpTextPrefix, "Default reconcile period for controllers"), " "),
	)
	flagSet.StringVar(&f.WatchesFile,
		"watches-file",
		"./watches.yaml",
		strings.Join(append(helpTextPrefix, "Path to the watches file to use"), " "),
	)
}
