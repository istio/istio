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

package util

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang/sync/errgroup"
	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/log"
)

const (
	// retry budget
	budget = 90
)

// Status represents a completion status for a retriable function
type Status error

var (
	// ErrAgain defines a retriable error.
	ErrAgain = Status(errors.New("try again"))
)

// TODO(nmittler): Can we remove this?

// Parallel runs the given functions in parallel with retries. All funcs must succeed for the function to succeed
func Parallel(fs map[string]func() Status) error {
	g, ctx := errgroup.WithContext(context.Background())
	repeat := func(name string, f func() Status) func() error {
		return func() error {
			for n := 0; n < budget; n++ {
				log.Infof("%s (attempt %d)", name, n)
				err := f()
				switch err {
				case nil:
					// success
					return nil
				case ErrAgain:
					// do nothing
				default:
					return fmt.Errorf("failed %s at attempt %d: %v", name, n, err)
				}
				select {
				case <-time.After(time.Second):
					// try again
				case <-ctx.Done():
					return nil
				}
			}
			return fmt.Errorf("failed all %d attempts for %s", budget, name)
		}
	}
	for name, f := range fs {
		g.Go(repeat(name, f))
	}
	return g.Wait()
}

// TODO(nmittler): Can we remove this?

// Repeat will reattempt the given function up to budget times or until it does not return an error
func Repeat(f func() error, budget int, delay time.Duration) error {
	var errs error
	for i := 0; i < budget; i++ {
		err := f()
		if err == nil {
			return nil
		}
		errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("attempt %d", i)))
		log.Infof("attempt #%d failed with %v", i, err)
		time.Sleep(delay)
	}
	return errs
}

// Tlog is a utility function that prints a progress message for the currently running test
func Tlog(header, s string) {
	log.Infof("\n\n=================== %s =====================\n%s\n\n", header, s)
}
