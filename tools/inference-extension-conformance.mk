# Version variables - can be overridden via environment or command line
# Example: make all ISTIO_VERSION=1.28.0
GATEWAY_API_VERSION ?= v1.3.0
INFERENCE_EXTENSION_VERSION ?= v1.3.0
ISTIO_VERSION ?= 1.30.0-rc.0
ISTIO_HUB ?=
ISTIO_PROFILE ?= ambient

# Conformance test variables
IMPLEMENTATION_VERSION ?= $(ISTIO_VERSION)
MODE ?= default
PROFILE ?= gateway
ORGANIZATION ?= istio
PROJECT ?= istio
URL ?= https://istio.io
CONTACT ?= @istio/maintainers
RUN_TEST ?=

# Directory variables
# Path to your local clone of gateway-api-inference-extension/conformance.
TEST_BASE_DIR ?= 
REPORT_BASE_DIR ?= 

# Kubernetes version
KUBERNETES_VERSION ?= v1.31.9

# Internal variables (derived from KUBERNETES_VERSION)
KIND_NODE_IMAGE ?= kindest/node:$(KUBERNETES_VERSION)
MINIKUBE_K8S_VERSION ?= $(KUBERNETES_VERSION)

# Istioctl binary variables
ISTIOCTL_DIR ?= /tmp
ISTIOCTL_BIN ?= $(ISTIOCTL_DIR)/istioctl-$(ISTIO_VERSION)

# YAML content for readability
define METALLB_CONFIG
apiVersion: v1
kind: ConfigMap
data:
  config: |
    address-pools:
    - name: default
      protocol: layer2
      addresses:
      - NETWORK_PREFIX.100-NETWORK_PREFIX.200
metadata:
  name: config
  namespace: metallb-system
endef

# Multi-line YAML configuration for metallb (kind)
define METALLB_KIND_CONFIG
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: example
  namespace: metallb-system
spec:
  addresses:
  - NETWORK_PREFIX.200-NETWORK_PREFIX.250
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: empty
  namespace: metallb-system
endef

define TLS_DESTINATION_RULES
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: primary-endpoint-picker-tls
  namespace: inference-conformance-app-backend
spec:
  host: primary-endpoint-picker-svc
  trafficPolicy:
      tls:
        mode: SIMPLE
        insecureSkipVerify: true
---
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: secondary-endpoint-picker-tls
  namespace: inference-conformance-app-backend
spec:
  host: secondary-endpoint-picker-svc
  trafficPolicy:
      tls:
        mode: SIMPLE
        insecureSkipVerify: true
---
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: dp-endpoint-picker-tls
  namespace: inference-conformance-app-backend
spec:
  host: dp-endpoint-picker-svc
  trafficPolicy:
      tls:
        mode: SIMPLE
        insecureSkipVerify: true
---
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: appprotocol-h2c-endpoint-picker-tls
  namespace: inference-conformance-app-backend
spec:
  host: appprotocol-h2c-endpoint-picker-svc
  trafficPolicy:
      tls:
        mode: SIMPLE
        insecureSkipVerify: true
---
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: appprotocol-http-endpoint-picker-tls
  namespace: inference-conformance-app-backend
spec:
  host: appprotocol-http-endpoint-picker-svc
  trafficPolicy:
      tls:
        mode: SIMPLE
        insecureSkipVerify: true
endef

# README template for implementation conformance reports
define README_TEMPLATE
# $(PROJECT) ($(PROFILE) Profile Conformance) - $(INFERENCE_EXTENSION_VERSION)

## Test Results

This directory contains conformance test results for Gateway API Inference Extension $(INFERENCE_EXTENSION_VERSION) testing against $(PROJECT) implementations using the $(PROFILE) profile.

| Extension Version Tested | Profile Tested | Implementation Version | Mode    | Report | Status |
|--------------------------|----------------|------------------------|---------|--------|--------|
| ...                      | ...            | ...                    | ...     | ...    | ...    |

## Running the Tests

For instructions on how to reproduce these test results and run the conformance tests yourself, see the [$(PROJECT) Conformance Testing README](../../../../scripts/$(PROJECT)/README.md).

## About This Version

- **Extension Version**: $(INFERENCE_EXTENSION_VERSION)
- **Profile**: $(PROFILE)
- **Implementation**: $(PROJECT)
- **Test Mode**: Default

For detailed information about conformance testing, report generation, and requirements, see the [main conformance README](../../../../../README.md).
endef

.PHONY: setup-env-kind setup-kind setup-istio setup-istio-kind setup-gateway-api-crds setup-inference-extension-crds setup-crds setup-tls run-tests readme-update clean clean-reports help ensure-report-dir check-test-base-dir check-report-base-dir

# Show help information
help:
	@echo "Gateway API Inference Extension Conformance Test Setup"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "📋 QUICK SETUP - All-in-one targets"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "  setup-env-kind                   - Setup complete environment with kind"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "🐳 KIND ENVIRONMENT - For local development with containers"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "  setup-kind                       - Setup kind cluster with metallb"
	@echo "  setup-istio-kind                 - Install Istio for kind environment"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "⚙️  COMMON SETUP - Environment-agnostic targets"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "  setup-istio                      - Install Istio (auto-downloads istioctl)"
	@echo "  setup-gateway-api-crds           - Install Gateway API CRDs"
	@echo "  setup-inference-extension-crds   - Install Inference Extension CRDs"
	@echo "  setup-crds                       - Install all CRDs"
	@echo "  setup-tls                        - Setup TLS for EPP"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "🧪 TESTING & REPORTING - Test execution and documentation"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "  run-tests                        - Run conformance tests"
	@echo "  readme-update                    - Update README table with all available reports"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "🧹 CLEANUP - Resource cleanup"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "  clean                            - Clean up resources"
	@echo "  clean-reports                    - Clean up generated reports and README table"
	@echo ""
	@echo "Main variables (can be overridden):"
	@echo "  GATEWAY_API_VERSION              - Gateway API version (current: $(GATEWAY_API_VERSION))"
	@echo "  INFERENCE_EXTENSION_VERSION      - Inference Extension version (current: $(INFERENCE_EXTENSION_VERSION))"
	@echo "  KUBERNETES_VERSION               - Kubernetes version for kind (current: $(KUBERNETES_VERSION))"
	@echo "  ISTIO_VERSION                    - Istio version (current: $(ISTIO_VERSION))"
	@echo "  PROFILE                          - Conformance profile (current: $(PROFILE))"
	@echo "  PROJECT                          - Project name (current: $(PROJECT))"
	@echo ""
	@echo "📁 REPORT STRUCTURE:"
	@echo "  Reports will be generated in: $(REPORT_BASE_DIR)/"
	@echo "  This structure is based on: reports/[INFERENCE_EXTENSION_VERSION]/[PROFILE]/[PROJECT]/"
	@echo ""
	@echo "📥 Automatic istioctl Management:"
	@echo "  • istioctl binary will be automatically downloaded based on ISTIO_VERSION"
	@echo "  • Hub (container registry) will be auto-detected from download source:"
	@echo "    - GitHub release → registry.istio.io/release"
	@echo "    - Container extraction → registry.istio.io/testing"
	@echo "  • Override with ISTIO_HUB if needed for custom registries"
	@echo ""
	@echo "Conformance test variables (can be overridden):"
	@echo "  MODE                             - Test mode (current: $(MODE))"
	@echo "  ORGANIZATION                     - Organization name (current: $(ORGANIZATION))"
	@echo "  URL                              - Project URL (current: $(URL))"
	@echo "  CONTACT                          - Contact information (current: $(CONTACT))"
	@echo "  RUN_TEST                         - Run specific test (current: $(RUN_TEST))"

	@echo ""
	@echo "Internal variables (advanced users only):"
	@echo "  IMPLEMENTATION_VERSION           - Implementation version for report (current: $(IMPLEMENTATION_VERSION), derived from ISTIO_VERSION)"
	@echo "  ISTIO_HUB                        - Istio container registry hub (current: $(ISTIO_HUB), auto-detected if empty)"
	@echo "  ISTIO_PROFILE                    - Istio profile (current: $(ISTIO_PROFILE))"
	@echo "  TEST_BASE_DIR                    - Test suite base directory (current: $(TEST_BASE_DIR))"
	@echo "  REPORT_BASE_DIR                  - Report output directory (current: $(REPORT_BASE_DIR))"
	@echo "  ISTIOCTL_DIR                     - Directory for istioctl binaries (current: $(ISTIOCTL_DIR))"
	@echo "  ISTIOCTL_BIN                     - Path to versioned istioctl binary (current: $(ISTIOCTL_BIN))"
	@echo "  KIND_NODE_IMAGE                  - Kind node image (current: $(KIND_NODE_IMAGE))"
	@echo "  MINIKUBE_K8S_VERSION             - Minikube K8s version (current: $(MINIKUBE_K8S_VERSION))"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "💡 USAGE EXAMPLES"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@echo "📋 Quick Setup Examples:"
	@echo "  make setup-env-kind                        - Use default versions with kind"
	@echo "  make setup-env-kind ISTIO_VERSION=1.28.0 - Override Istio version"
	@echo "  make setup-env-kind KUBERNETES_VERSION=v1.31.9 - Override Kubernetes version"
	@echo ""
	@echo "🐳 Kind Examples:"
	@echo "  make setup-kind                            - Setup kind cluster with metallb"
	@echo "  make setup-istio-kind                      - Install Istio for kind environment"
	@echo "  make setup-kind KUBERNETES_VERSION=v1.31.9 - Use Kubernetes 1.31.9"
	@echo ""
	@echo "⚙️  Common Setup Examples:"
	@echo "  make setup-istio ISTIO_PROFILE=ambient   - Install Istio with specific profile"
	@echo "  make setup-istio ISTIO_HUB=docker.io/istio - Install Istio from custom registry"
	@echo "  make setup-crds                            - Install all CRDs"
	@echo ""
	@echo "🧪 Testing Examples:"
	@echo "  make run-tests                             - Run conformance tests with default settings"
	@echo "  make run-tests TEST_BASE_DIR=..            - Run tests from custom directory"
	@echo "  make run-tests RUN_TEST=InferencePoolAccepted - Run specific test only"
	@echo "  make run-tests PROJECT=envoy PROFILE=mesh  - Run tests for different project/profile"
	@echo "  make readme-update                         - Update README table with all available reports"
	@echo ""

# Fail fast with a short message if TEST_BASE_DIR is unset
check-test-base-dir:
	@if [ -z "$(TEST_BASE_DIR)" ]; then \
		echo "missing TEST_BASE_DIR (set it to your local clone of kubernetes-sigs/gateway-api-inference-extension)"; \
		exit 1; \
	fi

# Fail fast with a short message if REPORT_BASE_DIR is unset
check-report-base-dir:
	@if [ -z "$(REPORT_BASE_DIR)" ]; then \
		echo "missing REPORT_BASE_DIR"; \
		exit 1; \
	fi

# Setup complete environment with kind
setup-env-kind: setup-kind setup-istio-kind setup-gateway-api-crds setup-inference-extension-crds setup-tls

# Ensure report directory exists
ensure-report-dir: check-report-base-dir
	@echo "Ensuring report directory exists: $(REPORT_BASE_DIR)"
	@mkdir -p $(REPORT_BASE_DIR)

# Setup kind with metallb
setup-kind:
	@echo "Setting up kind with metallb..."
	@if ! kind get clusters | grep -q "^kind$$"; then \
		echo "Creating kind cluster with Kubernetes $(KUBERNETES_VERSION)..."; \
		kind create cluster --image $(KIND_NODE_IMAGE); \
	else \
		echo "Kind cluster already exists"; \
	fi
	@echo "Installing metallb..."
	kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.13.7/config/manifests/metallb-native.yaml
	@echo "Waiting for metallb to be ready..."
	kubectl wait --namespace metallb-system --for=condition=available deployment/controller --timeout=90s
	kubectl wait --namespace metallb-system --for=condition=ready pod --selector=component=controller --timeout=90s
	kubectl wait --namespace metallb-system --for=condition=ready pod --selector=component=speaker --timeout=90s
	@echo "Configuring metallb address pool..."
	$(file >/tmp/metallb-kind-config.yaml,$(METALLB_KIND_CONFIG))
	@DOCKER_NETWORK=$$(docker network inspect -f '{{range .IPAM.Config}}{{.Gateway}} {{end}}' kind | grep -oE '([0-9]+\.){3}[0-9]+' | head -1); \
	NETWORK_PREFIX=$${DOCKER_NETWORK%.*}; \
	echo "Using IP range: $${NETWORK_PREFIX}.200-$${NETWORK_PREFIX}.250"; \
	sed -i.bak "s/NETWORK_PREFIX/$${NETWORK_PREFIX}/g" /tmp/metallb-kind-config.yaml && rm -f /tmp/metallb-kind-config.yaml.bak
	kubectl apply -f /tmp/metallb-kind-config.yaml
	@rm -f /tmp/metallb-kind-config.yaml

# Download istioctl binary for the specific version (file target)
$(ISTIOCTL_BIN):
	@echo "Downloading istioctl version $(ISTIO_VERSION)..."
	@mkdir -p $(ISTIOCTL_DIR)
	@OS=$$(uname -s | tr '[:upper:]' '[:lower:]'); \
	case "$$OS" in \
		darwin) OS=osx ;; \
	esac; \
	ARCH=$$(uname -m); \
	case "$$ARCH" in \
		x86_64) ARCH=amd64 ;; \
		aarch64) ARCH=arm64 ;; \
		armv7l) ARCH=armv7 ;; \
	esac; \
	echo "Detecting OS: $$OS, Architecture: $$ARCH"; \
	SUCCESS=false; \
	echo "Trying GitHub releases..."; \
	if curl -fsSL "https://github.com/istio/istio/releases/download/$(ISTIO_VERSION)/istioctl-$(ISTIO_VERSION)-$$OS-$$ARCH.tar.gz" -o /tmp/istioctl.tar.gz 2>/dev/null; then \
		echo "Download successful, extracting..."; \
		if tar -xzf /tmp/istioctl.tar.gz -C /tmp 2>/dev/null; then \
			mv /tmp/istioctl $(ISTIOCTL_BIN); \
			rm -f /tmp/istioctl.tar.gz; \
			chmod +x $(ISTIOCTL_BIN); \
			echo "istioctl $(ISTIO_VERSION) downloaded to $(ISTIOCTL_BIN)"; \
			echo "github" > $(ISTIOCTL_BIN).source; \
			SUCCESS=true; \
		else \
			echo "ERROR: Failed to extract istioctl archive"; \
			rm -f /tmp/istioctl.tar.gz; \
		fi; \
	fi; \
	if [ "$$SUCCESS" = "false" ]; then \
		echo "Binary download failed, trying container-based extraction..."; \
		CONTAINER_IMAGE="registry.istio.io/testing/istioctl:$(ISTIO_VERSION)"; \
		echo "Pulling container image: $$CONTAINER_IMAGE"; \
		if docker pull "$$CONTAINER_IMAGE" 2>/dev/null; then \
			echo "Extracting istioctl binary from container..."; \
			TEMP_CONTAINER=$$(docker create "$$CONTAINER_IMAGE"); \
			if docker cp "$$TEMP_CONTAINER:/usr/local/bin/istioctl" "$(ISTIOCTL_BIN)" 2>/dev/null; then \
				docker rm "$$TEMP_CONTAINER" 2>/dev/null || true; \
				chmod +x "$(ISTIOCTL_BIN)"; \
				echo "istioctl $(ISTIO_VERSION) extracted from container to $(ISTIOCTL_BIN)"; \
				echo "container" > $(ISTIOCTL_BIN).source; \
				SUCCESS=true; \
			else \
				echo "ERROR: Failed to extract istioctl binary from container"; \
				docker rm "$$TEMP_CONTAINER" 2>/dev/null || true; \
			fi; \
		else \
			echo "ERROR: Failed to pull container image $$CONTAINER_IMAGE"; \
		fi; \
	fi; \
	if [ "$$SUCCESS" = "false" ]; then \
		echo "ERROR: All download methods failed for istioctl $(ISTIO_VERSION)"; \
		echo "Tried: 1) GitHub releases, 2) Container extraction"; \
		echo "Please check if the version exists or install istioctl manually"; \
		exit 1; \
	fi
	@$(ISTIOCTL_BIN) version --remote=false

# Generate README file for implementation conformance reports (file target)
$(REPORT_BASE_DIR)/README.md: Makefile | ensure-report-dir
	@echo "Generating README for $(PROJECT) conformance reports..."
	@echo "Report directory: $(REPORT_BASE_DIR)"
	$(file >/tmp/readme-template.md,$(README_TEMPLATE))
	@sed -e 's/$$(PROJECT)/$(PROJECT)/g' \
		-e 's/$$(PROFILE)/$(PROFILE)/g' \
		-e 's/$$(INFERENCE_EXTENSION_VERSION)/$(INFERENCE_EXTENSION_VERSION)/g' \
		/tmp/readme-template.md > $@
	@rm -f /tmp/readme-template.md
	@echo "README generated at: $@"

# Install Istio (generic target using ISTIO_PROFILE variable)
setup-istio: $(ISTIOCTL_BIN)
	@echo "Installing Istio version $(ISTIO_VERSION) with profile $(ISTIO_PROFILE)..."
	@if [ -n "$(ISTIO_HUB)" ]; then \
		HUB="$(ISTIO_HUB)"; \
		echo "Using specified hub: $$HUB"; \
	elif [ -f "$(ISTIOCTL_BIN).source" ]; then \
		SOURCE=$$(cat $(ISTIOCTL_BIN).source); \
		if [ "$$SOURCE" = "github" ]; then \
			HUB="registry.istio.io/release"; \
			echo "Using hub for GitHub release: $$HUB"; \
		elif [ "$$SOURCE" = "container" ]; then \
			HUB="registry.istio.io/testing"; \
			echo "Using hub for container image: $$HUB"; \
		else \
			HUB="registry.istio.io/testing"; \
			echo "Unknown source, defaulting to testing hub: $$HUB"; \
		fi; \
	else \
		HUB="registry.istio.io/release"; \
		echo "WARNING: Could not detect istioctl source, defaulting to release hub: $$HUB"; \
		echo "If this is incorrect, specify ISTIO_HUB manually"; \
	fi; \
	echo "Installing Istio from $$HUB..."; \
	$(ISTIOCTL_BIN) install -y --set profile=$(ISTIO_PROFILE) --set values.global.hub=$$HUB --set values.global.tag=$(ISTIO_VERSION) --set values.pilot.env.SUPPORT_GATEWAY_API_INFERENCE_EXTENSION=true --set values.pilot.env.ENABLE_GATEWAY_API_INFERENCE_EXTENSION=true

# Install Istio for kind environment
setup-istio-kind: ISTIO_PROFILE := ambient
setup-istio-kind: setup-istio

# Apply Gateway API CRDs
setup-gateway-api-crds:
	@echo "Applying Gateway API CRDs version $(GATEWAY_API_VERSION)..."
	kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/$(GATEWAY_API_VERSION)/standard-install.yaml

# Apply Inference Extension CRDs
setup-inference-extension-crds:
	@echo "Applying Inference Extension CRDs version $(INFERENCE_EXTENSION_VERSION)..."
	kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/gateway-api-inference-extension/refs/tags/$(INFERENCE_EXTENSION_VERSION)/config/crd/bases/inference.networking.k8s.io_inferencepools.yaml
	kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/gateway-api-inference-extension/refs/tags/$(INFERENCE_EXTENSION_VERSION)/config/crd/bases/inference.networking.x-k8s.io_inferenceobjectives.yaml

# Apply all CRDs (convenience target)
setup-crds: setup-gateway-api-crds setup-inference-extension-crds

# Setup TLS for EPP
setup-tls:
	@echo "Setting up TLS for EPP..."
	-kubectl create namespace inference-conformance-app-backend || true
	$(file >/tmp/tls-destination-rules.yaml,$(TLS_DESTINATION_RULES))
	kubectl apply -f /tmp/tls-destination-rules.yaml
	@rm -f /tmp/tls-destination-rules.yaml

# Run conformance tests
run-tests: check-test-base-dir ensure-report-dir
	@echo "Running conformance tests..."
	@echo "Test base directory: $(TEST_BASE_DIR)"
	@echo "Report will be saved as: $(REPORT_BASE_DIR)/$(IMPLEMENTATION_VERSION)-$(MODE)-$(PROFILE)-report.yaml"
	@if [ -n "$(RUN_TEST)" ]; then \
		echo "Running specific test: $(RUN_TEST)"; \
	else \
		echo "Running all conformance tests"; \
	fi
	cd $(TEST_BASE_DIR) && go test ./conformance -args -gateway-class istio \
		-cleanup-base-resources=false \
		-report-output=$(shell pwd)/$(REPORT_BASE_DIR)/$(IMPLEMENTATION_VERSION)-$(MODE)-$(PROFILE)-report.yaml \
		-organization="$(ORGANIZATION)" \
		-project="$(PROJECT)" \
		-url="$(URL)" \
		-contact="$(CONTACT)" \
		-version="$(IMPLEMENTATION_VERSION)" \
		-mode="$(MODE)" \
		-run-test="$(RUN_TEST)"
	@echo ""
	@echo "Test completed! Report saved to: $(REPORT_BASE_DIR)/$(IMPLEMENTATION_VERSION)-$(MODE)-$(PROFILE)-report.yaml"
	@echo "To update the README table, run:"
	@echo "  make readme-update"

# Clean up resources
clean:
	@echo "Cleaning up..."
	kubectl delete namespace inference-conformance-app-backend --ignore-not-found=true
	@echo "Cleaning up downloaded istioctl binaries..."
	@rm -f $(ISTIOCTL_DIR)/istioctl-*
	@echo "Note: If using kind, run 'kind delete cluster' to completely clean up"
	@echo "Note: Run 'make clean-reports' to clean up generated reports and README table"

# Clean up generated conformance reports
clean-reports: check-report-base-dir
	@echo "Cleaning reports from: $(REPORT_BASE_DIR)"
	rm -f $(REPORT_BASE_DIR)/*-*-*-report.yaml
	@echo "Cleaning up README..."
	rm -f $(REPORT_BASE_DIR)/README.md
	@echo "Conformance reports and README for $(PROJECT) cleaned up"
	@echo "Note: Run 'make readme-update' to regenerate the README with current $(REPORT_BASE_DIR) reports"
