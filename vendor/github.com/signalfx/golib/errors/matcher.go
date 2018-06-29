package errors

// MatcherFunc is used to match errors
type MatcherFunc func(error) bool

// Matches should return true if the error matches the wrapped function
func (m MatcherFunc) Matches(err error) bool {
	return m(err)
}

// A Matcher detects if errors match some condition
type Matcher interface {
	Matches(err error) bool
}

// Matches is used to wrap the Cause() and is similar to something like:
//
//   f, err := do_something()
//   if Matches(err, os.IsTimeout) {
//     // It was a timeout error somewhere...
//   }
func Matches(err error, f func(error) bool) bool {
	return MatchesI(err, MatcherFunc(f))
}

// MatchesI is like Matches but takes the interface, if you need it.
func MatchesI(err error, m Matcher) bool {
	return m.Matches(Cause(err))
}
