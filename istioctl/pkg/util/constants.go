package util

const (
	// DefaultProxyAdminPort is the default port for the proxy admin server
	DefaultProxyAdminPort = 15000

	// DefaultMeshConfigMapName is the default name of the ConfigMap with the mesh config
	// The actual name can be different - use getMeshConfigMapName
	DefaultMeshConfigMapName = "istio"

	// ConfigMapKey should match the expected MeshConfig file name
	ConfigMapKey = "mesh"

	// ValuesConfigMapKey should match the expected Values file name
	ValuesConfigMapKey = "values"
)

const (
	// ExperimentalMsg indicate active development and not for production use warning.
	ExperimentalMsg = `THIS COMMAND IS UNDER ACTIVE DEVELOPMENT AND NOT READY FOR PRODUCTION USE.`
)
