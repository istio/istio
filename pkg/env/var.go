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

// Package env makes it possible to track use of environment variables within a process
// in order to generate documentation for these uses.
package env

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"istio.io/istio/pkg/log"
)

// The type of a variable's value
type VarType byte

const (
	// Variable holds a free-form string.
	STRING VarType = iota
	// Variable holds a boolean value.
	BOOL
	// Variable holds a signed integer.
	INT
	// Variables holds a floating point value.
	FLOAT
	// Variable holds a time duration.
	DURATION
	// Variable holds a dynamic unknown type.
	OTHER
)

// Var describes a single environment variable
type Var struct {
	// The name of the environment variable.
	Name string

	// The optional default value of the environment variable.
	DefaultValue string

	// Description of the environment variable's purpose.
	Description string

	// Hide the existence of this variable when outputting usage information.
	Hidden bool

	// Mark this variable as deprecated when generating usage information.
	Deprecated bool

	// The type of the variable's value
	Type VarType

	// The underlying Go type of the variable
	GoType string
}

// StringVar represents a single string environment variable.
type StringVar struct {
	Var
}

// BoolVar represents a single boolean environment variable.
type BoolVar struct {
	Var
}

// IntVar represents a single integer environment variable.
type IntVar struct {
	Var
}

// FloatVar represents a single floating-point environment variable.
type FloatVar struct {
	Var
}

// DurationVar represents a single duration environment variable.
type DurationVar struct {
	Var
}

var (
	allVars = make(map[string]Var)
	mutex   sync.Mutex
)

// VarDescriptions returns a description of this process' environment variables, sorted by name.
func VarDescriptions() []Var {
	mutex.Lock()
	sorted := make([]Var, 0, len(allVars))
	for _, v := range allVars {
		sorted = append(sorted, v)
	}
	mutex.Unlock()

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	return sorted
}

type GenericVar[T comparable] struct {
	Var
	delegate specializedVar[T]
}

func Register[T comparable](name string, defaultValue T, description string) GenericVar[T] {
	// Specialized cases
	// In the future, once only Register() remains, we can likely drop most of these.
	// However, time.Duration is needed still as it doesn't implement json.
	switch d := any(defaultValue).(type) {
	case time.Duration:
		v := RegisterDurationVar(name, d, description)
		return GenericVar[T]{v.Var, any(v).(specializedVar[T])}
	case string:
		v := RegisterStringVar(name, d, description)
		return GenericVar[T]{v.Var, any(v).(specializedVar[T])}
	case float64:
		v := RegisterFloatVar(name, d, description)
		return GenericVar[T]{v.Var, any(v).(specializedVar[T])}
	case int:
		v := RegisterIntVar(name, d, description)
		return GenericVar[T]{v.Var, any(v).(specializedVar[T])}
	case bool:
		v := RegisterBoolVar(name, d, description)
		return GenericVar[T]{v.Var, any(v).(specializedVar[T])}
	}
	b, _ := json.Marshal(defaultValue)
	v := Var{Name: name, DefaultValue: string(b), Description: description, Type: STRING, GoType: fmt.Sprintf("%T", defaultValue)}
	RegisterVar(v)
	return GenericVar[T]{getVar(name), nil}
}

// RegisterStringVar registers a new string environment variable.
func RegisterStringVar(name string, defaultValue string, description string) StringVar {
	v := Var{Name: name, DefaultValue: defaultValue, Description: description, Type: STRING}
	RegisterVar(v)
	return StringVar{getVar(name)}
}

// RegisterBoolVar registers a new boolean environment variable.
func RegisterBoolVar(name string, defaultValue bool, description string) BoolVar {
	v := Var{Name: name, DefaultValue: strconv.FormatBool(defaultValue), Description: description, Type: BOOL}
	RegisterVar(v)
	return BoolVar{getVar(name)}
}

// RegisterIntVar registers a new integer environment variable.
func RegisterIntVar(name string, defaultValue int, description string) IntVar {
	v := Var{Name: name, DefaultValue: strconv.FormatInt(int64(defaultValue), 10), Description: description, Type: INT}
	RegisterVar(v)
	return IntVar{getVar(name)}
}

// RegisterFloatVar registers a new floating-point environment variable.
func RegisterFloatVar(name string, defaultValue float64, description string) FloatVar {
	v := Var{Name: name, DefaultValue: strconv.FormatFloat(defaultValue, 'G', -1, 64), Description: description, Type: FLOAT}
	RegisterVar(v)
	return FloatVar{v}
}

// RegisterDurationVar registers a new duration environment variable.
func RegisterDurationVar(name string, defaultValue time.Duration, description string) DurationVar {
	v := Var{Name: name, DefaultValue: defaultValue.String(), Description: description, Type: DURATION}
	RegisterVar(v)
	return DurationVar{getVar(name)}
}

// RegisterVar registers a generic environment variable.
func RegisterVar(v Var) {
	mutex.Lock()

	if old, ok := allVars[v.Name]; ok {
		if v.Description != "" {
			allVars[v.Name] = v // last one with a description wins if the same variable name is registered multiple times
		}

		if old.Description != v.Description || old.DefaultValue != v.DefaultValue || old.Type != v.Type || old.Deprecated != v.Deprecated || old.Hidden != v.Hidden {
			log.Warnf("The environment variable %s was registered multiple times using different metadata: %v, %v", v.Name, old, v)
		}
	} else {
		allVars[v.Name] = v
	}

	mutex.Unlock()
}

func getVar(name string) Var {
	mutex.Lock()
	result := allVars[name]
	mutex.Unlock()

	return result
}

// Get retrieves the value of the environment variable.
// It returns the value, which will be the default if the variable is not present.
// To distinguish between an empty value and an unset value, use Lookup.
func (v StringVar) Get() string {
	result, _ := v.Lookup()
	return result
}

// Lookup retrieves the value of the environment variable. If the
// variable is present in the environment the
// value (which may be empty) is returned and the boolean is true.
// Otherwise the returned value will be the default and the boolean will
// be false.
func (v StringVar) Lookup() (string, bool) {
	result, ok := os.LookupEnv(v.Name)
	if !ok {
		result = v.DefaultValue
	}

	return result, ok
}

// Get retrieves the value of the environment variable.
// It returns the value, which will be the default if the variable is not present.
// To distinguish between an empty value and an unset value, use Lookup.
func (v BoolVar) Get() bool {
	result, _ := v.Lookup()
	return result
}

// Lookup retrieves the value of the environment variable. If the
// variable is present in the environment the
// value (which may be empty) is returned and the boolean is true.
// Otherwise the returned value will be the default and the boolean will
// be false.
func (v BoolVar) Lookup() (bool, bool) {
	result, ok := os.LookupEnv(v.Name)
	if !ok {
		result = v.DefaultValue
	}

	b, err := strconv.ParseBool(result)
	if err != nil {
		log.Warnf("Invalid environment variable value `%s`, expecting true/false, defaulting to %v", result, v.DefaultValue)
		b, _ = strconv.ParseBool(v.DefaultValue)
	}

	return b, ok
}

// Get retrieves the value of the environment variable.
// It returns the value, which will be the default if the variable is not present.
// To distinguish between an empty value and an unset value, use Lookup.
func (v IntVar) Get() int {
	result, _ := v.Lookup()
	return result
}

// Lookup retrieves the value of the environment variable. If the
// variable is present in the environment the
// value (which may be empty) is returned and the boolean is true.
// Otherwise the returned value will be the default and the boolean will
// be false.
func (v IntVar) Lookup() (int, bool) {
	result, ok := os.LookupEnv(v.Name)
	if !ok {
		result = v.DefaultValue
	}

	i, err := strconv.Atoi(result)
	if err != nil {
		log.Warnf("Invalid environment variable value `%s`, expecting an integer, defaulting to %v", result, v.DefaultValue)
		i, _ = strconv.Atoi(v.DefaultValue)
	}

	return i, ok
}

// Get retrieves the value of the environment variable.
// It returns the value, which will be the default if the variable is not present.
// To distinguish between an empty value and an unset value, use Lookup.
func (v FloatVar) Get() float64 {
	result, _ := v.Lookup()
	return result
}

// Lookup retrieves the value of the environment variable. If the
// variable is present in the environment the
// value (which may be empty) is returned and the boolean is true.
// Otherwise the returned value will be the default and the boolean will
// be false.
func (v FloatVar) Lookup() (float64, bool) {
	result, ok := os.LookupEnv(v.Name)
	if !ok {
		result = v.DefaultValue
	}

	f, err := strconv.ParseFloat(result, 64)
	if err != nil {
		log.Warnf("Invalid environment variable value `%s`, expecting a floating-point value, defaulting to %v", result, v.DefaultValue)
		f, _ = strconv.ParseFloat(v.DefaultValue, 64)
	}

	return f, ok
}

// Get retrieves the value of the environment variable.
// It returns the value, which will be the default if the variable is not present.
// To distinguish between an empty value and an unset value, use Lookup.
func (v DurationVar) Get() time.Duration {
	result, _ := v.Lookup()
	return result
}

// Lookup retrieves the value of the environment variable. If the
// variable is present in the environment the
// value (which may be empty) is returned and the boolean is true.
// Otherwise the returned value will be the default and the boolean will
// be false.
func (v DurationVar) Lookup() (time.Duration, bool) {
	result, ok := os.LookupEnv(v.Name)
	if !ok {
		result = v.DefaultValue
	}

	d, err := time.ParseDuration(result)
	if err != nil {
		log.Warnf("Invalid environment variable value `%s`, expecting a duration, defaulting to %v", result, v.DefaultValue)
		d, _ = time.ParseDuration(v.DefaultValue)
	}

	return d, ok
}

// Get retrieves the value of the environment variable.
// It returns the value, which will be the default if the variable is not present.
// To distinguish between an empty value and an unset value, use Lookup.
func (v GenericVar[T]) Get() T {
	if v.delegate != nil {
		return v.delegate.Get()
	}
	result, _ := v.Lookup()
	return result
}

// Lookup retrieves the value of the environment variable. If the
// variable is present in the environment the
// value (which may be empty) is returned and the boolean is true.
// Otherwise the returned value will be the default and the boolean will
// be false.
func (v GenericVar[T]) Lookup() (T, bool) {
	if v.delegate != nil {
		return v.delegate.Lookup()
	}
	result, ok := os.LookupEnv(v.Name)
	if !ok {
		result = v.DefaultValue
	}

	res := new(T)

	if err := json.Unmarshal([]byte(result), res); err != nil {
		log.Warnf("Invalid environment variable value `%s` defaulting to %v: %v", result, v.DefaultValue, err)
		_ = json.Unmarshal([]byte(v.DefaultValue), res)
	}

	return *res, ok
}

func (v GenericVar[T]) IsSet() bool {
	_, ok := v.Lookup()
	return ok
}

func (v GenericVar[T]) GetName() string {
	return v.Var.Name
}

// specializedVar represents a var that can Get/Lookup
type specializedVar[T any] interface {
	Lookup() (T, bool)
	Get() T
}

// VariableInfo provides generic information about a variable. All Variables implement this interface.
// This is largely to workaround lack of covariance in Go.
type VariableInfo interface {
	GetName() string
	IsSet() bool
}
