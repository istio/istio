// Copyright 2019 Istio Authors
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

package validate

import (
	"fmt"
	"net"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"istio.io/operator/pkg/util"

	"istio.io/pkg/log"
)

var (
	// DebugPackage controls verbose debugging in this package. Used for offline debugging.
	DebugPackage = false

	// alphaNumericRegexp defines the alpha numeric atom, typically a
	// component of names. This only allows lower case characters and digits.
	alphaNumericRegexp = match(`[a-z0-9]+`)

	// separatorRegexp defines the separators allowed to be embedded in name
	// components. This allow one period, one or two underscore and multiple
	// dashes.
	separatorRegexp = match(`(?:[._]|__|[-]*)`)

	// nameComponentRegexp restricts registry path component names to start
	// with at least one letter or number, with following parts able to be
	// separated by one period, one or two underscore and multiple dashes.
	nameComponentRegexp = expression(
		alphaNumericRegexp,
		optional(repeated(separatorRegexp, alphaNumericRegexp)))

	// domainComponentRegexp restricts the registry domain component of a
	// repository name to start with a component as defined by DomainRegexp
	// and followed by an optional port.
	domainComponentRegexp = match(`(?:[a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])`)

	// DomainRegexp defines the structure of potential domain components
	// that may be part of image names. This is purposely a subset of what is
	// allowed by DNS to ensure backwards compatibility with Docker image
	// names.
	DomainRegexp = expression(
		domainComponentRegexp,
		optional(repeated(literal(`.`), domainComponentRegexp)),
		optional(literal(`:`), match(`[0-9]+`)))

	// TagRegexp matches valid tag names. From docker/docker:graph/tags.go.
	TagRegexp = match(`[\w][\w.-]{0,127}`)

	// DigestRegexp matches valid digests.
	DigestRegexp = match(`[A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]{32,}`)

	// NameRegexp is the format for the name component of references. The
	// regexp has capturing groups for the domain and name part omitting
	// the separating forward slash from either.
	NameRegexp = expression(
		optional(DomainRegexp, literal(`/`)),
		nameComponentRegexp,
		optional(repeated(literal(`/`), nameComponentRegexp)))

	// ReferenceRegexp is the full supported format of a reference. The regexp
	// is anchored and has capturing groups for name, tag, and digest
	// components.
	ReferenceRegexp = anchored(capture(NameRegexp),
		optional(literal(":"), capture(TagRegexp)),
		optional(literal("@"), capture(DigestRegexp)))

	// ObjectNameRegexp is a legal name for a k8s object.
	ObjectNameRegexp = match(`[a-z0-9.-]{1,254}`)
)

// validateWithRegex checks whether the given value matches the regexp r.
func validateWithRegex(path util.Path, val interface{}, r *regexp.Regexp) (errs util.Errors) {
	switch {
	case !isString(val):
		errs = util.AppendErr(errs, fmt.Errorf("path %s has bad type %T, want string", path, val))

	case len(r.FindString(val.(string))) != len(val.(string)):
		errs = util.AppendErr(errs, fmt.Errorf("invalid value %s: %s", path, val))
	}

	printError(errs.ToError())
	return errs
}

// validateStringList returns a validator function that works on a string list, using the supplied ValidatorFunc vf on
// each element.
func validateStringList(vf ValidatorFunc) ValidatorFunc {
	return func(path util.Path, val interface{}) util.Errors {
		dbgPrintC("validateStringList(")
		if reflect.TypeOf(val).Kind() != reflect.String {
			err := fmt.Errorf("validateStringList %s got %T, want string", path, val)
			printError(err)
			return util.NewErrs(err)
		}
		var errs util.Errors
		for _, s := range strings.Split(val.(string), ",") {
			errs = util.AppendErrs(errs, vf(path, strings.TrimSpace(s)))
			dbgPrint("\nerrors(%d): %v", len(errs), errs)
		}
		printError(errs.ToError())
		return errs
	}
}

// validatePortNumberString checks if val is a string with a valid port number.
func validatePortNumberString(path util.Path, val interface{}) util.Errors {
	dbgPrintC("validatePortNumberString %v: ", val)
	if !isString(val) {
		return util.NewErrs(fmt.Errorf("validatePortNumberString(%s) bad type %T, want string", path, val))
	}
	intV, err := strconv.ParseInt(val.(string), 10, 32)
	if err != nil {
		return util.NewErrs(fmt.Errorf("%s : %s", path, err))
	}
	return validatePortNumber(path, intV)
}

// validatePortNumber checks whether val is an integer representing a valid port number.
func validatePortNumber(path util.Path, val interface{}) util.Errors {
	return validateIntRange(path, val, 0, 65535)
}

// validateIntRange checks whether val is an integer in [min, max].
func validateIntRange(path util.Path, val interface{}, min, max int64) util.Errors {
	dbgPrintC("validateIntRange %s:%v in [%d, %d]?: ", path, val, min, max)
	k := reflect.TypeOf(val).Kind()
	var err error
	switch {
	case util.IsIntKind(k):
		v := reflect.ValueOf(val).Int()
		if v < min || v > max {
			err = fmt.Errorf("value %s:%v falls outside range [%v, %v]", path, v, min, max)
		}
	case util.IsUintKind(k):
		v := reflect.ValueOf(val).Uint()
		if int64(v) < min || int64(v) > max {
			err = fmt.Errorf("value %s:%v falls out side range [%v, %v]", path, v, min, max)
		}
	default:
		err = fmt.Errorf("validateIntRange %s unexpected type %T, want int type", path, val)
	}
	printError(err)
	return util.NewErrs(err)
}

// validateCIDR checks whether val is a string with a valid CIDR.
func validateCIDR(path util.Path, val interface{}) util.Errors {
	dbgPrintC("validateCIDR (%s): ", val)
	var err error
	if reflect.TypeOf(val).Kind() != reflect.String {
		err = fmt.Errorf("validateCIDR %s got %T, want string", path, val)
	} else {
		_, _, err = net.ParseCIDR(val.(string))
		if err != nil {
			err = fmt.Errorf("%s %s", path, err)
		}
	}
	printError(err)
	return util.NewErrs(err)
}

func isString(val interface{}) bool {
	return reflect.TypeOf(val).Kind() == reflect.String
}

func printError(err error) {
	if !DebugPackage {
		return
	}
	if err == nil {
		fmt.Println("OK")
		return
	}
	fmt.Println(err)
}

func dbgPrint(v ...interface{}) {
	if !DebugPackage {
		return
	}
	dbgPrintC(v...)
	log.Infof("")
}

func dbgPrintC(v ...interface{}) {
	if !DebugPackage {
		return
	}
	log.Infof(fmt.Sprintf(v[0].(string), v[1:]...))
}

// match compiles the string to a regular expression.
var match = regexp.MustCompile

// literal compiles s into a literal regular expression, escaping any regexp
// reserved characters.
func literal(s string) *regexp.Regexp {
	re := match(regexp.QuoteMeta(s))

	if _, complete := re.LiteralPrefix(); !complete {
		panic("must be a literal")
	}

	return re
}

// expression defines a full expression, where each regular expression must
// follow the previous.
func expression(res ...*regexp.Regexp) *regexp.Regexp {
	var s string
	for _, re := range res {
		s += re.String()
	}

	return match(s)
}

// optional wraps the expression in a non-capturing group and makes the
// production optional.
func optional(res ...*regexp.Regexp) *regexp.Regexp {
	return match(group(expression(res...)).String() + `?`)
}

// repeated wraps the regexp in a non-capturing group to get one or more
// matches.
func repeated(res ...*regexp.Regexp) *regexp.Regexp {
	return match(group(expression(res...)).String() + `+`)
}

// group wraps the regexp in a non-capturing group.
func group(res ...*regexp.Regexp) *regexp.Regexp {
	return match(`(?:` + expression(res...).String() + `)`)
}

// capture wraps the expression in a capturing group.
func capture(res ...*regexp.Regexp) *regexp.Regexp {
	return match(`(` + expression(res...).String() + `)`)
}

// anchored anchors the regular expression by adding start and end delimiters.
func anchored(res ...*regexp.Regexp) *regexp.Regexp {
	return match(`^` + expression(res...).String() + `$`)
}

// ValidatorFunc validates a value.
type ValidatorFunc func(path util.Path, i interface{}) util.Errors
