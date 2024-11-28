package bootstrap

import (
	"io/ioutil"
	"math"
	"strconv"

	"google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/istio/pkg/ali/config/constants"
)

// DetermineConcurrencyOption determines the correct setting for --concurrency based on CPU requests/limits
func DetermineConcurrencyOption() *wrapperspb.Int32Value {
	// If limit is set, us that
	// The format in the file is a plain integer. `100` in the file is equal to `100m` (based on `divisor: 1m`
	// in the pod spec).
	// With the resource setting, we round up to single integer number; for example, if we have a 500m limit
	// the pod will get concurrency=1. With 6500m, it will get concurrency=7.
	limit, err := ReadPodCPULimits()
	if err == nil && limit > 0 {
		return &wrapperspb.Int32Value{Value: int32(math.Ceil(float64(limit) / 1000))}
	}
	// If limit is unset, use requests instead, with the same logic.
	requests, err := ReadPodCPURequests()
	if err == nil && requests > 0 {
		return &wrapperspb.Int32Value{Value: int32(math.Ceil(float64(requests) / 1000))}
	}
	return nil
}

func ReadPodCPURequests() (int, error) {
	b, err := ioutil.ReadFile(constants.PodInfoCPURequestsPath)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(b))
}

func ReadPodCPULimits() (int, error) {
	b, err := ioutil.ReadFile(constants.PodInfoCPULimitsPath)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(b))
}
