# Tiltfile for Istio control plane (istiod) development
# Builds pilot-discovery, deploys to Kind via Helm, and live-reloads on Go source changes.
# Modeled after the AgentGateway Tiltfile pattern.
#
# Prerequisites:
#   - Tilt v0.33+ (https://docs.tilt.dev/install.html)
#   - Kind cluster with a local registry (use ctlptl):
#       ctlptl create registry ctlptl-registry --port=5000
#       ctlptl create cluster kind --registry=ctlptl-registry
#   - Go 1.24+
#
# Usage:
#   tilt up              # Start dev environment
#   tilt up --stream     # Start with inline logs
#   tilt down            # Tear down
#
# See tools/tilt/README.md for full documentation.

load('ext://restart_process', 'docker_build_with_restart')
load('ext://helm_resource', 'helm_resource', 'helm_repo')

# =============================================================================
# Configuration
# =============================================================================

version = 'dev'
cluster_name = 'kind'
install_namespace = 'istio-system'
image_registry = 'localhost:5000'
image_name = image_registry + '/pilot'

# Only allow connections to Kind clusters to prevent accidental prod deploys
allow_k8s_contexts('kind-' + cluster_name)

# =============================================================================
# Setup: Ensure cluster is ready
# =============================================================================

# Check if Kind cluster exists
if str(local('kind get clusters 2>/dev/null | grep -c "^' + cluster_name + '$" || true')).strip() == '0':
    print('No Kind cluster found! Create one with a local registry and restart Tilt.')
    print('  ctlptl create registry ctlptl-registry --port=5000')
    print('  ctlptl create cluster kind --registry=ctlptl-registry')
    fail('No Kind cluster. Create one and run tilt again.')

# Ensure istio-system namespace exists
local('kubectl create namespace ' + install_namespace + ' --dry-run=client -o yaml | kubectl apply -f -')

# =============================================================================
# Install Istio CRDs (istio-base chart)
# =============================================================================

print('Installing Istio base CRDs...')
helm_resource(
    'istio-base',
    'manifests/charts/base',
    namespace=install_namespace,
    flags=[
        '--set=global.istioNamespace=' + install_namespace,
    ],
)

# =============================================================================
# Build pilot-discovery (istiod control plane)
# =============================================================================

local_resource(
    'go-compile-istiod',
    'GOOS=linux GOARCH=$(go env GOARCH) go build '
        + '-gcflags="all=-N -l" '
        + '-tags=vtprotobuf,disable_pgv '
        + '-o ./tools/tilt/pilot-discovery '
        + './pilot/cmd/pilot-discovery',
    deps=[
        './pilot/',
        './pkg/',
        './go.mod',
        './go.sum',
    ],
    ignore=[
        './**/*_test.go',
        './pilot/docker/',
        './pilot/test/',
    ],
)

# =============================================================================
# Docker image with live update
# =============================================================================

docker_build_with_restart(
    image_name,
    context='./tools/tilt/',
    entrypoint=['/usr/local/bin/pilot-discovery', 'discovery'],
    dockerfile_contents="""
FROM ubuntu:24.04
RUN useradd -u 1337 -m istio-proxy
COPY pilot-discovery /usr/local/bin/pilot-discovery
USER 1337:1337
ENTRYPOINT ["/usr/local/bin/pilot-discovery"]
    """,
    live_update=[
        sync('./tools/tilt/pilot-discovery', '/usr/local/bin/pilot-discovery'),
    ],
    only=[
        './pilot-discovery',
    ],
)

# =============================================================================
# Deploy istiod via Helm
# =============================================================================

k8s_yaml(helm(
    'manifests/charts/istio-control/istio-discovery',
    name='istiod',
    namespace=install_namespace,
    set=[
        'pilot.image=' + image_name,
        'pilot.hub=' + image_registry,
        'pilot.tag=' + version,
        'global.hub=' + image_registry,
        'global.tag=' + version,
        'global.imagePullPolicy=Always',
        'global.istioNamespace=' + install_namespace,
        'pilot.replicaCount=1',
        'pilot.autoscaleEnabled=false',
    ],
))

k8s_resource(
    'istiod',
    resource_deps=['go-compile-istiod', 'istio-base'],
    port_forwards=[
        port_forward(8080, 8080, name='debug'),
        port_forward(15010, 15010, name='grpc-xds'),
        port_forward(15014, 15014, name='metrics'),
    ],
)
