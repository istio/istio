package ambient

import (
	"context"
	"encoding/base64"
	"fmt"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/framework/components/ambient"
	"istio.io/istio/pkg/test/framework/components/echo"
	cdeployment "istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/registryredirector"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/scopes"
)

type EchoDeployments struct {
	// Namespace echo apps will be deployed
	Namespace         namespace.Instance
	ExternalNamespace namespace.Instance

	// AllWaypoint is a waypoint for all types
	AllWaypoint echo.Instances
	// WorkloadAddressedWaypoint is a workload only waypoint
	WorkloadAddressedWaypoint echo.Instances
	// ServiceAddressedWaypoint is a serviceonly waypoint
	ServiceAddressedWaypoint echo.Instances
	// Captured echo service
	Captured echo.Instances
	// Uncaptured echo Service
	Uncaptured echo.Instances
	// Sidecar echo services with sidecar
	Sidecar echo.Instances

	// All echo services
	All echo.Instances
	// Echo services that are in the mesh
	Mesh echo.Instances
	// Echo services that are not in mesh
	MeshExternal echo.Instances

	MockExternal echo.Instances

	// WaypointProxies by
	WaypointProxies map[string]ambient.Waypoints
}

const (
	WorkloadAddressedWaypoint = "workload-addressed-waypoint"
	ServiceAddressedWaypoint  = "service-addressed-waypoint"
	Captured                  = "captured"
	Uncaptured                = "uncaptured"
	Sidecar                   = "sidecar"
	Global                    = "global"
	Local                     = "local"
	EastWestGateway           = "eastwest-gateway"
)

var inMesh = match.Matcher(func(instance echo.Instance) bool {
	return instance.Config().HasProxyCapabilities()
})

func SetupApps(t resource.Context, i istio.Instance, apps *EchoDeployments) error {
	var err error
	apps.Namespace, err = namespace.New(t, namespace.Config{
		Prefix: "echo",
		Inject: false,
		Labels: map[string]string{
			label.IoIstioDataplaneMode.Name: "ambient",
		},
	})
	if err != nil {
		return err
	}
	apps.ExternalNamespace, err = namespace.New(t, namespace.Config{
		Prefix: "external",
		Inject: false,
		Labels: map[string]string{
			"istio.io/test-exclude-namespace": "true",
		},
	})
	if err != nil {
		return err
	}

	// Headless services don't work with targetPort, set to same port
	headlessPorts := make([]echo.Port, len(ports.All()))
	for i, p := range ports.All() {
		p.ServicePort = p.WorkloadPort
		headlessPorts[i] = p
	}
	builder := deployment.New(t).
		WithClusters(t.Clusters()...).
		WithConfig(echo.Config{
			Service:               WorkloadAddressedWaypoint,
			Namespace:             apps.Namespace,
			Ports:                 ports.All(),
			ServiceAccount:        true,
			WorkloadWaypointProxy: "waypoint",
			Subsets: []echo.SubsetConfig{
				{
					Replicas: 1,
					Version:  "v1",
					Labels: map[string]string{
						"app":                         WorkloadAddressedWaypoint,
						"version":                     "v1",
						label.IoIstioUseWaypoint.Name: "waypoint",
					},
				},
				{
					Replicas: 1,
					Version:  "v2",
					Labels: map[string]string{
						"app":                         WorkloadAddressedWaypoint,
						"version":                     "v2",
						label.IoIstioUseWaypoint.Name: "waypoint",
					},
				},
			},
		}).
		WithConfig(echo.Config{
			Service:              ServiceAddressedWaypoint,
			Namespace:            apps.Namespace,
			Ports:                ports.All(),
			ServiceLabels:        map[string]string{label.IoIstioUseWaypoint.Name: "waypoint"},
			ServiceAccount:       true,
			ServiceWaypointProxy: "waypoint",
			Subsets: []echo.SubsetConfig{
				{
					Replicas: 1,
					Version:  "v1",
					Labels: map[string]string{
						"app":     ServiceAddressedWaypoint,
						"version": "v1",
					},
				},
				{
					Replicas: 1,
					Version:  "v2",
					Labels: map[string]string{
						"app":     ServiceAddressedWaypoint,
						"version": "v2",
					},
				},
			},
		}).
		WithConfig(echo.Config{
			Service:        Captured,
			Namespace:      apps.Namespace,
			Ports:          ports.All(),
			ServiceAccount: true,
			Subsets: []echo.SubsetConfig{
				{
					Replicas: 1,
					Version:  "v1",
				},
				{
					Replicas: 1,
					Version:  "v2",
				},
			},
		}).
		WithConfig(echo.Config{
			Service:        Uncaptured,
			Namespace:      apps.Namespace,
			Ports:          ports.All(),
			ServiceAccount: true,
			Subsets: []echo.SubsetConfig{
				{
					Replicas: 1,
					Version:  "v1",
					Labels:   map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeNone},
				},
				{
					Replicas: 1,
					Version:  "v2",
					Labels:   map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeNone},
				},
			},
		})

	_, whErr := t.Clusters().Default().
		Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().
		Get(context.Background(), "istio-sidecar-injector", metav1.GetOptions{})
	if whErr != nil && !kerrors.IsNotFound(whErr) {
		return whErr
	}
	// Only setup sidecar tests if webhook is installed
	if whErr == nil {
		builder = builder.WithConfig(echo.Config{
			Service:        Sidecar,
			Namespace:      apps.Namespace,
			Ports:          ports.All(),
			ServiceAccount: true,
			Subsets: []echo.SubsetConfig{
				{
					Replicas: 1,
					Version:  "v1",
					Labels: map[string]string{
						"sidecar.istio.io/inject":       "true",
						label.IoIstioDataplaneMode.Name: constants.DataplaneModeNone,
					},
				},
				{
					Replicas: 1,
					Version:  "v2",
					Labels: map[string]string{
						"sidecar.istio.io/inject":       "true",
						label.IoIstioDataplaneMode.Name: constants.DataplaneModeNone,
					},
				},
			},
		})
	}

	external := cdeployment.External{Namespace: apps.ExternalNamespace}
	external.Build(t, builder)

	echos, err := builder.Build()
	if err != nil {
		return err
	}
	for _, b := range echos {
		scopes.Framework.Infof("built %v", b.Config().Service)
	}

	external.LoadValues(echos)
	apps.MockExternal = external.All

	// All does not include external
	echos = match.Not(match.ServiceName(echo.NamespacedName{Name: cdeployment.ExternalSvc, Namespace: apps.ExternalNamespace})).GetMatches(echos)
	apps.All = echos
	apps.WorkloadAddressedWaypoint = match.ServiceName(echo.NamespacedName{Name: WorkloadAddressedWaypoint, Namespace: apps.Namespace}).GetMatches(echos)
	apps.ServiceAddressedWaypoint = match.ServiceName(echo.NamespacedName{Name: ServiceAddressedWaypoint, Namespace: apps.Namespace}).GetMatches(echos)
	apps.AllWaypoint = apps.AllWaypoint.Append(apps.WorkloadAddressedWaypoint)
	apps.AllWaypoint = apps.AllWaypoint.Append(apps.ServiceAddressedWaypoint)
	apps.Uncaptured = match.ServiceName(echo.NamespacedName{Name: Uncaptured, Namespace: apps.Namespace}).GetMatches(echos)
	apps.Captured = match.ServiceName(echo.NamespacedName{Name: Captured, Namespace: apps.Namespace}).GetMatches(echos)
	apps.Sidecar = match.ServiceName(echo.NamespacedName{Name: Sidecar, Namespace: apps.Namespace}).GetMatches(echos)
	apps.Mesh = inMesh.GetMatches(echos)
	apps.MeshExternal = match.Not(inMesh).GetMatches(echos)

	if err := cdeployment.DeployExternalServiceEntry(t.ConfigIstio(), apps.Namespace, apps.ExternalNamespace, false).
		Apply(apply.CleanupConditionally); err != nil {
		return err
	}

	if apps.WaypointProxies == nil {
		apps.WaypointProxies = make(map[string]ambient.Waypoints)
	}

	for _, echo := range echos {
		svcwp := echo.Config().ServiceWaypointProxy
		wlwp := echo.Config().WorkloadWaypointProxy
		if svcwp != "" {
			if _, found := apps.WaypointProxies[svcwp]; !found {
				apps.WaypointProxies[svcwp], err = ambient.NewWaypointProxy(t, apps.Namespace, svcwp)
				if err != nil {
					return err
				}
			}
		}
		if wlwp != "" {
			if _, found := apps.WaypointProxies[wlwp]; !found {
				apps.WaypointProxies[wlwp], err = ambient.NewWaypointProxy(t, apps.Namespace, wlwp)
				if err != nil {
					return err
				}
			}
		}

	}

	return nil
}

var registry registryredirector.Instance

const (
	// Same user name and password as specified at pkg/test/fakes/imageregistry
	registryUser   = "user"
	registryPasswd = "passwd"
)

func TestRegistrySetup(apps *EchoDeployments, ctx resource.Context) (err error) {
	var config registryredirector.Config

	isKind := ctx.Clusters().IsKindCluster()

	// By default, for any platform, the test will pull the test image from public "gcr.io" registry.
	// For kind clusters, it will pull images from the "kind-registry" local registry container. In IPv6-only clusters
	// this is not supported as CoreDNS will attempt to "talk" to the upstream DNS over docker/IPv4 for resolution.
	// For context: https://github.com/kubernetes-sigs/kind/issues/3114
	// and https://github.com/istio/istio/pull/51778
	for _, c := range ctx.Clusters() {
		if isKind {
			config = registryredirector.Config{
				Cluster:        c,
				TargetRegistry: "kind-registry:5000",
				Scheme:         "http",
			}
		}

		registry, err = registryredirector.New(ctx, config)
		if err != nil {
			return err
		}

		args := map[string]any{
			"DockerConfigJson": base64.StdEncoding.EncodeToString(
				[]byte(createDockerCredential(registryUser, registryPasswd, registry.Address()))),
		}
		if err := ctx.ConfigIstio().EvalFile(apps.Namespace.Name(), args, "testdata/registry-secret.yaml").
			Apply(apply.CleanupConditionally); err != nil {
			return err
		}
	}
	return nil
}

func createDockerCredential(user, passwd, registry string) string {
	credentials := `{
	"auths":{
		"%v":{
			"username": "%v",
			"password": "%v",
			"email": "test@example.com",
			"auth": "%v"
		}
	}
}`
	auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + passwd))
	return fmt.Sprintf(credentials, registry, user, passwd, auth)
}