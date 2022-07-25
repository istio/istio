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

package validate

import (
	"fmt"
	"net"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/util"
	"istio.io/pkg/log"
)

var (
	scope = log.RegisterScope("validation", "API validation", 0)

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
func validateWithRegex(path util.Path, val any, r *regexp.Regexp) (errs util.Errors) {
	valStr := fmt.Sprint(val)
	if len(r.FindString(valStr)) != len(valStr) {
		errs = util.AppendErr(errs, fmt.Errorf("invalid value %s: %v", path, val))
		printError(errs.ToError())
	}
	return errs
}

// validateStringList returns a validator function that works on a string list, using the supplied ValidatorFunc vf on
// each element.
func validateStringList(vf ValidatorFunc) ValidatorFunc {
	return func(path util.Path, val any) util.Errors {
		msg := fmt.Sprintf("validateStringList %v", val)
		if !util.IsString(val) {
			err := fmt.Errorf("validateStringList %s got %T, want string", path, val)
			printError(err)
			return util.NewErrs(err)
		}
		var errs util.Errors
		for _, s := range strings.Split(val.(string), ",") {
			errs = util.AppendErrs(errs, vf(path, strings.TrimSpace(s)))
			scope.Debugf("\nerrors(%d): %v", len(errs), errs)
			msg += fmt.Sprintf("\nerrors(%d): %v", len(errs), errs)
		}
		logWithError(errs.ToError(), msg)
		return errs
	}
}

// validatePortNumberString checks if val is a string with a valid port number.
func validatePortNumberString(path util.Path, val any) util.Errors {
	scope.Debugf("validatePortNumberString %v:", val)
	if !util.IsString(val) {
		return util.NewErrs(fmt.Errorf("validatePortNumberString(%s) bad type %T, want string", path, val))
	}
	if val.(string) == "*" || val.(string) == "" {
		return nil
	}
	intV, err := strconv.ParseInt(val.(string), 10, 32)
	if err != nil {
		return util.NewErrs(fmt.Errorf("%s : %s", path, err))
	}
	return validatePortNumber(path, intV)
}

// validatePortNumber checks whether val is an integer representing a valid port number.
func validatePortNumber(path util.Path, val any) util.Errors {
	return validateIntRange(path, val, 0, 65535)
}

// validateIPRangesOrStar validates IP ranges and also allow star, examples: "1.1.0.256/16,2.2.0.257/16", "*"
func validateIPRangesOrStar(path util.Path, val any) (errs util.Errors) {
	scope.Debugf("validateIPRangesOrStar at %v: %v", path, val)

	if !util.IsString(val) {
		err := fmt.Errorf("validateIPRangesOrStar %s got %T, want string", path, val)
		printError(err)
		return util.NewErrs(err)
	}

	if val.(string) == "*" || val.(string) == "" {
		return errs
	}

	return validateStringList(validateCIDR)(path, val)
}

// validateIntRange checks whether val is an integer in [min, max].
func validateIntRange(path util.Path, val any, min, max int64) util.Errors {
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
	logWithError(err, "validateIntRange %s:%v in [%d, %d]?: ", path, val, min, max)
	return util.NewErrs(err)
}

// validateCIDR checks whether val is a string with a valid CIDR.
func validateCIDR(path util.Path, val any) util.Errors {
	var err error
	if !util.IsString(val) {
		err = fmt.Errorf("validateCIDR %s got %T, want string", path, val)
	} else {
		_, _, err = net.ParseCIDR(val.(string))
		if err != nil {
			err = fmt.Errorf("%s %s", path, err)
		}
	}
	logWithError(err, "validateCIDR (%s): ", val)
	return util.NewErrs(err)
}

func printError(err error) {
	if err == nil {
		scope.Debug("OK")
		return
	}
	scope.Debugf("%v", err)
}

// logWithError prints debug log with err message
func logWithError(err error, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if err == nil {
		msg += ": OK\n"
	} else {
		msg += fmt.Sprintf(": %v\n", err)
	}
	scope.Debug(msg)
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
type ValidatorFunc func(path util.Path, i any) util.Errors

// UnmarshalIOP unmarshals a string containing IstioOperator as YAML.
func UnmarshalIOP(iopYAML string) (*v1alpha1.IstioOperator, error) {
	// Remove creationDate (util.UnmarshalWithJSONPB fails if present)
	mapIOP := make(map[string]any)
	if err := yaml.Unmarshal([]byte(iopYAML), &mapIOP); err != nil {
		return nil, err
	}
	// Don't bother trying to remove the timestamp if there are no fields.
	// This also preserves iopYAML if it is ""; we don't want iopYAML to be the string "null"
	if len(mapIOP) > 0 {
		un := &unstructured.Unstructured{Object: mapIOP}
		un.SetCreationTimestamp(meta_v1.Time{}) // UnmarshalIstioOperator chokes on these
		iopYAML = util.ToYAML(un)
	}
	iop := &v1alpha1.IstioOperator{}

	if err := yaml.UnmarshalStrict([]byte(iopYAML), iop); err != nil {
		return nil, fmt.Errorf("%s:\n\nYAML:\n%s", err, iopYAML)
	}
	return iop, nil
}

// ValidIOP validates the given IstioOperator object.
func ValidIOP(iop *v1alpha1.IstioOperator) error {
	errs := CheckIstioOperatorSpec(iop.Spec, false)
	return errs.ToError()
}

// compose path for slice s with index i
func indexPathForSlice(s string, i int) string {
	return fmt.Sprintf("%s[%d]", s, i)
}

// get validation function for specified path
func getValidationFuncForPath(validations map[string]ValidatorFunc, path util.Path) (ValidatorFunc, bool) {
	pstr := path.String()
	// fast match
	if !strings.Contains(pstr, "[") && !strings.Contains(pstr, "]") {
		vf, ok := validations[pstr]
		return vf, ok
	}
	for p, vf := range validations {
		ps := strings.Split(p, ".")
		if len(ps) != len(path) {
			continue
		}
		for i, v := range ps {
			if !matchPathNode(v, path[i]) {
				break
			}
			if i == len(ps)-1 {
				return vf, true
			}
		}
	}
	return nil, false
}

// check whether the pn path node match pattern.
// pattern may container '*', eg. [1] match [*].
func matchPathNode(pattern, pn string) bool {
	if !strings.Contains(pattern, "[") && !strings.Contains(pattern, "]") {
		return pattern == pn
	}
	if !strings.Contains(pn, "[") && !strings.Contains(pn, "]") {
		return false
	}
	indexPattern := pattern[strings.IndexByte(pattern, '[')+1 : strings.IndexByte(pattern, ']')]
	if indexPattern == "*" {
		return true
	}
	index := pn[strings.IndexByte(pn, '[')+1 : strings.IndexByte(pn, ']')]
	return indexPattern == index
}
