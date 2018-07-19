// Copyright 2018 Istio Authors.
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

package rbac

import (
	"errors"
	"fmt"
	"strings"

	"istio.io/istio/mixer/template/authorization"
	"istio.io/istio/pkg/log"
)

// SubjectArgs contains information about the subject of a request.
type SubjectArgs struct {
	User       string
	Groups     string
	Properties []string
}

// ActionArgs contains information about the detail of a request.
type ActionArgs struct {
	Namespace  string
	Service    string
	Method     string
	Path       string
	Properties []string
}

// istioctlLogger is used redirect the mixer adapter log to standard log in istioctl.
type istioctlLogger struct{}

// Infof from adapter.Logger.
func (l istioctlLogger) Infof(format string, args ...interface{}) {
	// Redirect info to debug for istioctl.
	log.Debugf(format, args...)
}

// Warningf from adapter.Logger.
func (l istioctlLogger) Warningf(format string, args ...interface{}) {
	log.Warnf(format, args...)
}

// Errorf from adapter.Logger.
func (l istioctlLogger) Errorf(format string, args ...interface{}) error {
	s := fmt.Sprintf(format, args...)
	log.Errorf(s)
	return errors.New(s)
}

// Debugf from adapter.Logger.
func (l istioctlLogger) Debugf(format string, args ...interface{}) {
	log.Debugf(format, args...)
}

// InfoEnabled from adapter.Logger.
func (l istioctlLogger) InfoEnabled() bool {
	return false
}

// WarnEnabled from adapter.Logger.
func (l istioctlLogger) WarnEnabled() bool {
	return false
}

// ErrorEnabled from adapter.Logger.
func (l istioctlLogger) ErrorEnabled() bool {
	return false
}

// DebugEnabled from adapter.Logger.
func (l istioctlLogger) DebugEnabled() bool {
	return false
}

// Check performs the permission check for given subject on the given action.
func (rs *ConfigStore) Check(subject SubjectArgs, action ActionArgs) (bool, error) {
	instance, err := createInstance(subject, action)
	if err != nil {
		return false, err
	}
	return rs.CheckPermission(instance, istioctlLogger{})
}

func createInstance(subject SubjectArgs, action ActionArgs) (*authorization.Instance, error) {
	instance := &authorization.Instance{
		Subject: &authorization.Subject{
			User:       subject.User,
			Groups:     subject.Groups,
			Properties: map[string]interface{}{},
		},
		Action: &authorization.Action{
			Namespace:  action.Namespace,
			Service:    action.Service,
			Method:     action.Method,
			Path:       action.Path,
			Properties: map[string]interface{}{},
		},
	}
	if err := fillInstanceProperties(&instance.Subject.Properties, subject.Properties); err != nil {
		return nil, err
	}
	if err := fillInstanceProperties(&instance.Action.Properties, action.Properties); err != nil {
		return nil, err
	}

	return instance, nil
}

func fillInstanceProperties(properties *map[string]interface{}, arguments []string) error {
	for _, arg := range arguments {
		// Use the part before the first = as key and the remaining part as a string value, this is
		// the only supported format of RBAC adapter for now.
		split := strings.SplitN(arg, "=", 2)
		if len(split) != 2 {
			return fmt.Errorf("invalid property %v, the format should be: key=value", arg)
		}
		value, present := (*properties)[split[0]]
		if present {
			return fmt.Errorf("duplicate property %v, previous value %v", arg, value)
		}
		(*properties)[split[0]] = split[1]
	}
	return nil
}
