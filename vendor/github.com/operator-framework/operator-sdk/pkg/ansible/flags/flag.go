// Copyright 2018 The Operator-SDK Authors
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

	"github.com/operator-framework/operator-sdk/pkg/internal/flags"
	"github.com/operator-framework/operator-sdk/pkg/log/zap"
	"github.com/spf13/pflag"
)

// AnsibleOperatorFlags - Options to be used by an ansible operator
type AnsibleOperatorFlags struct {
	flags.WatchFlags
	InjectOwnerRef bool
	MaxWorkers     int
}

// AddTo - Add the ansible operator flags to the the flagset
// helpTextPrefix will allow you add a prefix to default help text. Joined by a space.
func AddTo(flagSet *pflag.FlagSet, helpTextPrefix ...string) *AnsibleOperatorFlags {
	aof := &AnsibleOperatorFlags{}
	aof.WatchFlags.AddTo(flagSet, helpTextPrefix...)
	flagSet.AddFlagSet(zap.FlagSet())
	flagSet.BoolVar(&aof.InjectOwnerRef,
		"inject-owner-ref",
		true,
		strings.Join(append(helpTextPrefix, "The ansible operator will inject owner references unless this flag is false"), " "),
	)
	flagSet.IntVar(&aof.MaxWorkers,
		"max-workers",
		1,
		strings.Join(append(helpTextPrefix,
			"Maximum number of workers to use. Overridden by environment variable."),
			" "),
	)
	return aof
}
