package main

import (
	"fmt"
	"regexp"
)

var (
	validK8sNameRegex = regexp.MustCompile(`[a-z0-9-.]+`)
)

func validateK8sName(s string) error {
	if !validK8sNameRegex.MatchString(s) {
		return fmt.Errorf("%s is not a valid k8s name, names can only contain alphanumeric characters, '-' or '.'", s)
	}
	return nil
}
