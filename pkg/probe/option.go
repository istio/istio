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
	"errors"
	"github.com/hashicorp/go-multierror"
	"time"
)

// Options customizes the parameters of a probe.
type Options struct {
	// Path defines the path to the file used for the existence.
	Path string

	// UpdateInterval defines the interval for updating the file's last modified
	// time.
	UpdateInterval time.Duration
}

// IsValid returns true if some values are filled into the options.
func (o *Options) Validate() error {
	if o == nil {
		return errors.New("option is nil")
	}
	var errs error
	if o.Path == "" {
		errs = errors.New("empty probe-path")
	}
	if o.UpdateInterval <= 0*time.Second {
		errs = multierror.Append(errs, errors.New("invalid interval"))
	}
	return errs
}
