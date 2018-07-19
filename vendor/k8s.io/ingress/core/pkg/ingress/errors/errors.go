/*
Copyright 2016 The Kubernetes Authors.

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

package errors

import (
	"fmt"

	"github.com/pkg/errors"
)

var (
	// ErrMissingAnnotations the ingress rule does not contain annotations
	// This is an error only when annotations are being parsed
	ErrMissingAnnotations = errors.New("ingress rule without annotations")

	// ErrInvalidAnnotationName the ingress rule does contains an invalid
	// annotation name
	ErrInvalidAnnotationName = errors.New("invalid annotation name")
)

// NewInvalidAnnotationContent returns a new InvalidContent error
func NewInvalidAnnotationContent(name string, val interface{}) error {
	return InvalidContent{
		Name: fmt.Sprintf("the annotation %v does not contain a valid value (%v)", name, val),
	}
}

// NewLocationDenied returns a new LocationDenied error
func NewLocationDenied(reason string) error {
	return LocationDenied{
		Reason: errors.Errorf("Location denied, reason: %v", reason),
	}
}

// InvalidContent error
type InvalidContent struct {
	Name string
}

func (e InvalidContent) Error() string {
	return e.Name
}

// LocationDenied error
type LocationDenied struct {
	Reason error
}

func (e LocationDenied) Error() string {
	return e.Reason.Error()
}

// IsLocationDenied checks if the err is an error which
// indicates a location should return HTTP code 503
func IsLocationDenied(e error) bool {
	_, ok := e.(LocationDenied)
	return ok
}

// IsMissingAnnotations checks if the err is an error which
// indicates the ingress does not contain annotations
func IsMissingAnnotations(e error) bool {
	return e == ErrMissingAnnotations
}

// IsInvalidContent checks if the err is an error which
// indicates an annotations value is not valid
func IsInvalidContent(e error) bool {
	_, ok := e.(InvalidContent)
	return ok
}
