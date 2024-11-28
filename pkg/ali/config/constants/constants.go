package constants

const (
	// PodInfoCPURequestsPath is the filepath that pod CPU requests will be stored
	// This is typically set by the downward API
	PodInfoCPURequestsPath = "./etc/istio/pod/cpu-request"

	// PodInfoCPULimitsPath is the filepath that pod CPU limits will be stored
	// This is typically set by the downward API
	PodInfoCPULimitsPath = "./etc/istio/pod/cpu-limit"
)
