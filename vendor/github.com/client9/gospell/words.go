package gospell

import (
	"regexp"
	"strings"
	"unicode"
)

// number form, may include dots, commas and dashes
var numberRegexp = regexp.MustCompile("^([0-9]+[.,-]?)+$")

// number form with units, e.g. 123ms, 12in  1ft
var numberUnitsRegexp = regexp.MustCompile("^[0-9]+[a-zA-Z]+$")

// 0x12FF or 0x1B or x12FF
// does anyone use 0XFF ??
var numberHexRegexp = regexp.MustCompile("^0?[x][0-9A-Fa-f]+$")

var numberBinaryRegexp = regexp.MustCompile("^0[b][01]+$")

var camelCaseRegexp1 = regexp.MustCompile("[A-Z]+")

var shaHashRegexp = regexp.MustCompile("^[0-9a-z]{40}$")

// Splitter splits a text into words
// Highly likely this implementation will change so we are encapsulating.
type Splitter struct {
	fn func(c rune) bool
}

// Split is the function to split an input into a `[]string`
func (s *Splitter) Split(in string) []string {
	return strings.FieldsFunc(in, s.fn)
}

// NewSplitter creates a new splitter.  The input is a string in
// UTF-8 encoding.  Each rune in the string will be considered to be a
// valid word character.  Runes that are NOT here are deemed a word
// boundary Current implementation uses
// https://golang.org/pkg/strings/#FieldsFunc
func NewSplitter(chars string) *Splitter {
	s := Splitter{}
	s.fn = (func(c rune) bool {
		// break if it's not a letter, and not another special character
		return !unicode.IsLetter(c) && -1 == strings.IndexRune(chars, c)
	})
	return &s
}

func isNumber(s string) bool {
	return numberRegexp.MatchString(s)
}

func isNumberBinary(s string) bool {
	return numberBinaryRegexp.MatchString(s)
}

// is word in the form of a "number with units", e.g. "101ms", "3ft",
// "5GB" if true, return the units, if not return empty string This is
// highly English based and not sure how applicable it is to other
// languages.
func isNumberUnits(s string) string {
	// regexp.FindAllStringSubmatch is too confusing
	if !numberUnitsRegexp.MatchString(s) {
		return ""
	}
	// Starts with a number
	for idx, ch := range s {
		if ch >= '0' && ch <= '9' {
			continue
		}
		return s[idx:]
	}
	panic("assertion failed")
}

func isNumberHex(s string) bool {
	return numberHexRegexp.MatchString(s)
}

func isHash(s string) bool {
	return shaHashRegexp.MatchString(s)
}

func splitCamelCase(s string) []string {
	out := []string{}

	s = strings.Replace(s, "HTTP", "Http", -1)
	s = strings.Replace(s, "HTML", "Html", -1)
	s = strings.Replace(s, "URL", "Url", -1)
	s = strings.Replace(s, "URI", "Uri", -1)

	caps := camelCaseRegexp1.FindAllStringIndex(s, -1)

	// all lower case
	if len(caps) == 0 {
		return nil
	}

	// is only first character capitalized? or is the whole word capitalized
	if len(caps) == 1 && caps[0][0] == 0 && (caps[0][1] == 1 || caps[0][1] == len(s)) {
		return nil
	}
	last := 0
	for i := 0; i < len(caps); i++ {
		if last != caps[i][0] {
			out = append(out, s[last:caps[i][0]])
			last = caps[i][0]
		}
		if caps[i][1]-caps[i][0] > 1 {
			out = append(out, s[caps[i][0]:caps[i][1]])
			last = caps[i][1]
		}
	}
	if last < len(s) {
		out = append(out, s[last:])
	}

	return out
}
