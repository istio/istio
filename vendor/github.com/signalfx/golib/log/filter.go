package log

import (
	"bytes"
	"encoding/json"
	"expvar"
	"github.com/signalfx/golib/errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
)

// A Filter controls if logging should happen.
type Filter interface {
	WouldLog(keyvals ...interface{}) bool
	Disableable
}

// MultiFilter logs to the first filter that passes.
type MultiFilter struct {
	Filters []Filter
	PassTo  Logger
}

// Disabled returns true if all inner filters are disabled
func (f *MultiFilter) Disabled() bool {
	if len(f.Filters) == 0 {
		return true
	}
	for _, filter := range f.Filters {
		if !filter.Disabled() {
			return false
		}
	}
	return true
}

// Log will log out to the first filter
func (f *MultiFilter) Log(keyvals ...interface{}) {
	if f.Disabled() {
		return
	}
	for _, filter := range f.Filters {
		if filter.WouldLog(keyvals...) {
			f.PassTo.Log(keyvals...)
			return
		}
	}
}

// RegexFilter controls logging to only happen to logs that have a dimension that matches all the regex
type RegexFilter struct {
	MissingValueKey   Key
	Log               Logger
	ErrCallback       ErrorHandler
	filterChangeIndex int64
	currentFilters    map[string]*regexp.Regexp
	mu                sync.RWMutex
}

// Disabled returns true if there are no current filters enabled.
func (f *RegexFilter) Disabled() bool {
	f.mu.RLock()
	if len(f.currentFilters) == 0 {
		f.mu.RUnlock()
		return true
	}
	f.mu.RUnlock()
	return false
}

// WouldLog returns true if the regex matches keyvals dimensions
func (f *RegexFilter) WouldLog(keyvals ...interface{}) bool {
	if f.Disabled() {
		return false
	}
	m := mapFromKeyvals(f.MissingValueKey, keyvals...)
	shouldPass := false
	f.mu.RLock()
	if matches(f.currentFilters, m, f.ErrCallback) {
		shouldPass = true
	}
	f.mu.RUnlock()
	if shouldPass {
		return true
	}
	return false
}

func (f *RegexFilter) varStats() interface{} {
	filters := f.GetFilters()
	ret := struct {
		Stats   Stats
		Filters map[string]string
	}{
		Filters: make(map[string]string, len(filters)),
	}
	for k, v := range filters {
		ret.Filters[k] = v.String()
	}
	ret.Stats = f.Stats()
	return ret
}

// Var is an expvar that exports various internal stats and the regex filters
func (f *RegexFilter) Var() expvar.Var {
	return expvar.Func(f.varStats)
}

// Stats describes the filter
type Stats struct {
	ChangeIndex int64
	FilterLen   int
}

// Stats returns a Stats object that describes the filter
func (f *RegexFilter) Stats() Stats {
	return Stats{
		ChangeIndex: atomic.LoadInt64(&f.filterChangeIndex),
		FilterLen:   len(f.GetFilters()),
	}
}

// GetFilters returns the currently used regex filters.  Do not modify this map.
func (f *RegexFilter) GetFilters() map[string]*regexp.Regexp {
	f.mu.RLock()
	ret := f.currentFilters
	f.mu.RUnlock()
	return ret
}

// FilterCount is a log key that is the count of current filters
var FilterCount = Key("filter_count")

// SetFilters changes the internal regex map
func (f *RegexFilter) SetFilters(filters map[string]*regexp.Regexp) {
	f.Log.Log(FilterCount, len(filters), "Changing filters")
	f.mu.Lock()
	f.currentFilters = filters
	atomic.AddInt64(&f.filterChangeIndex, 1)
	f.mu.Unlock()
}

func matches(filters map[string]*regexp.Regexp, m map[string]interface{}, onErr ErrorHandler) bool {
	if len(filters) == 0 {
		return false
	}
	buf := &bytes.Buffer{}
	for k, rgx := range filters {
		currentVal, exists := m[k]
		if !exists {
			return false
		}
		buf.Reset()
		if err := json.NewEncoder(buf).Encode(currentVal); err != nil {
			onErr.ErrorLogger(err).Log("Unable to encode a JSON value")
			return false
		}
		if !rgx.MatchString(strings.TrimSpace(buf.String())) {
			return false
		}
	}
	return true
}

// FilterChangeHandler allows changing filters via HTTP calls
type FilterChangeHandler struct {
	Filter *RegexFilter
	Log    Logger
}

var _ http.Handler = &FilterChangeHandler{}

// ServeHTTP will process the POST/GET request and change the current filters
func (f *FilterChangeHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	var postErr error
	if req.Method == "POST" {
		postErr = f.servePost(rw, req)
	}
	if req.Method == "GET" || req.Method == "POST" {
		f.serveGet(rw, req, postErr)
		return
	}
	http.NotFound(rw, req)
}

var getTemplate = template.Must(template.New("get request").Parse(
	`<?xml version="1.0" encoding="iso-8859-1"?>
<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "DTD/xhtml1-transitional.dtd">
<html xml:lang="en" lang="en" xmlns="http://www.w3.org/1999/xhtml">
	<head>
		<title>Current Filters</title>
   <style type="text/css">
    textarea
{
     font-family:Consolas,Monaco,Lucida Console,Liberation Mono,DejaVu Sans Mono,Bitstream Vera Sans Mono,Courier New, monospace;
}
	</style>
	</head>
	<body>
	  {{ if .PostErr }}<p>ERROR SETTING POST: {{ .PostErr}} </p>{{end}}
	  <h1>Existing filters</h1>
	    <p> There are currently {{ .FilterCount }} filters. </p>
	    <p> The filters have been changed {{ .ChangeIndex }} times. </p>
	    <form>
	    <p>
	    <h2>Put new filter here</h2>
	   <textarea name="newregex" rows="20" cols="120">{{ .CurrentRegex }}</textarea>
	   <h2>New filter duration</h2>
	   </p>
	   <p>
	   <button type="submit" formmethod="post">Change filters</button>
	   </p>
	   </form>
	   <h1>Usage</h1>
		<p>Input 1 or more filters into the box above where each filter is one line long.
		Filters are some key name, followed by a :, then the rest of the line which becomes
		a regex.  See examples below.</p>
		<h2>Find id matching 123</h2>
	    <textarea rows="3" cols="120" readonly="readonly">id:123</textarea>
	    <h2>Find names matching bob and ids matching 123 with anything after</h2>
	    <textarea rows="3" cols="120" readonly="readonly">name:bob
id:123.*</textarea>
	</body>
</html>`))

type getData struct {
	CurrentRegex string
	PostErr      error
	FilterCount  int
	ChangeIndex  int64
}

func (f *FilterChangeHandler) servePost(rw http.ResponseWriter, req *http.Request) error {
	if err := req.ParseForm(); err != nil {
		return errors.Annotate(err, "cannot parse POST form")
	}
	newRegex, err := FiltersFromString(req.Form.Get("newregex"))
	if err != nil {
		return err
	}
	f.Filter.SetFilters(newRegex)
	return nil
}

// FiltersFromString is a helper that creates regex style filters from a \n separated string of
// : separated id -> regex pairs.
func FiltersFromString(newRegex string) (map[string]*regexp.Regexp, error) {
	if newRegex == "" {
		return nil, nil
	}
	lines := strings.Split(newRegex, "\n")
	newFilters := make(map[string]*regexp.Regexp, len(lines))
	for _, line := range lines {
		lineParts := strings.SplitN(line, ":", 2)
		if len(lineParts) != 2 || lineParts[0] == "" || lineParts[1] == "" {
			return nil, errors.Errorf("Cannot parse line %s", line)
		}
		reg, err := regexp.Compile(lineParts[1])
		if err != nil {
			return nil, errors.Annotatef(err, "Cannot parse regex line %s", line)
		}
		newFilters[lineParts[0]] = reg
	}
	return newFilters, nil
}

func (f *FilterChangeHandler) serveGet(rw http.ResponseWriter, req *http.Request, postErr error) {
	allFilters := f.Filter.GetFilters()
	currentData := make([]string, 0, len(allFilters))
	for key, filter := range allFilters {
		currentData = append(currentData, key+":"+filter.String())
	}
	templateData := getData{
		CurrentRegex: strings.Join(currentData, "\n"),
		PostErr:      postErr,
		FilterCount:  len(allFilters),
		ChangeIndex:  atomic.LoadInt64(&f.Filter.filterChangeIndex),
	}
	rw.Header().Add("Content-Type", "application/xhtml+xml")
	if err := getTemplate.Execute(rw, templateData); err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		f.Log.Log(Err, err, "cannot execute template")
	}
}
