/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package log

import (
	"github.com/go-logr/logr"
)

// loggerPromise knows how to populate a concrete logr.Logger
// with options, given an actual base logger later on down the line.
type loggerPromise struct {
	logger        *DelegatingLogger
	childPromises []*loggerPromise

	name *string
	tags []interface{}
}

// WithName provides a new Logger with the name appended
func (p *loggerPromise) WithName(l *DelegatingLogger, name string) *loggerPromise {
	res := &loggerPromise{
		logger: l,
		name:   &name,
	}
	p.childPromises = append(p.childPromises, res)
	return res
}

// WithValues provides a new Logger with the tags appended
func (p *loggerPromise) WithValues(l *DelegatingLogger, tags ...interface{}) *loggerPromise {
	res := &loggerPromise{
		logger: l,
		tags:   tags,
	}
	p.childPromises = append(p.childPromises, res)
	return res
}

// Fulfill instantiates the Logger with the provided logger
func (p *loggerPromise) Fulfill(parentLogger logr.Logger) {
	var logger = parentLogger
	if p.name != nil {
		logger = logger.WithName(*p.name)
	}

	if p.tags != nil {
		logger = logger.WithValues(p.tags...)
	}

	p.logger.Logger = logger
	p.logger.promise = nil

	for _, childPromise := range p.childPromises {
		childPromise.Fulfill(logger)
	}
}

// DelegatingLogger is a logr.Logger that delegates to another logr.Logger.
// If the underlying promise is not nil, it registers calls to sub-loggers with
// the logging factory to be populated later, and returns a new delegating
// logger.  It expects to have *some* logr.Logger set at all times (generally
// a no-op logger before the promises are fulfilled).
type DelegatingLogger struct {
	logr.Logger
	promise *loggerPromise
}

// WithName provides a new Logger with the name appended
func (l *DelegatingLogger) WithName(name string) logr.Logger {
	if l.promise == nil {
		return l.Logger.WithName(name)
	}

	res := &DelegatingLogger{Logger: l.Logger}
	promise := l.promise.WithName(res, name)
	res.promise = promise

	return res
}

// WithValues provides a new Logger with the tags appended
func (l *DelegatingLogger) WithValues(tags ...interface{}) logr.Logger {
	if l.promise == nil {
		return l.Logger.WithValues(tags...)
	}

	res := &DelegatingLogger{Logger: l.Logger}
	promise := l.promise.WithValues(res, tags...)
	res.promise = promise

	return res
}

// Fulfill switches the logger over to use the actual logger
// provided, instead of the temporary initial one, if this method
// has not been previously called.
func (l *DelegatingLogger) Fulfill(actual logr.Logger) {
	if l.promise != nil {
		l.promise.Fulfill(actual)
	}
}

// NewDelegatingLogger constructs a new DelegatingLogger which uses
// the given logger before it's promise is fulfilled.
func NewDelegatingLogger(initial logr.Logger) *DelegatingLogger {
	l := &DelegatingLogger{
		Logger:  initial,
		promise: &loggerPromise{},
	}
	l.promise.logger = l
	return l
}
