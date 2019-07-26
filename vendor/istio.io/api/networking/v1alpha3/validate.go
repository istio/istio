package v1alpha3

import (
	"cuelang.org/go/cuego"
)

// ValidateGateway checks gateway specifications.
func ValidateGateway(name, namespace string, m Gateway) error {

	// TODO: Register the constraints to the current context,
	// so that cue will use the constraints defined in the gateway.json
	// file to validate.

	// Only validating the proto message in cuego.
	return cuego.Validate(m)
}
