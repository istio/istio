// Copyright (c) 2012-2016 Eli Janssen
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package statsd

import (
	"fmt"
	"regexp"
)

// The ValidatorFunc type defines a function that can serve
// as a stat name validation function.
type ValidatorFunc func(string) error

var safeName = regexp.MustCompile(`^[a-zA-Z0-9\-_.]+$`)

// CheckName may be used to validate whether a stat name contains invalid
// characters. If invalid characters are found, the function will return an
// error.
func CheckName(stat string) error {
	if !safeName.MatchString(stat) {
		return fmt.Errorf("invalid stat name: %s", stat)
	}
	return nil
}
