package controller

import (
	"istio.io/operator/pkg/controller/istiocontrolplane"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, istiocontrolplane.Add)
}
