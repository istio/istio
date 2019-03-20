WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

# shellcheck source=prow/lib.sh
source "${ROOT}/prow/lib.sh"
setup_kind_cluster


E2E_ARGS+=("--test_logs_path=${ARTIFACTS_DIR}")
# e2e tests on prow use clusters borrowed from boskos, which cleans up the
# clusters. There is no need to cleanup in the test jobs.
E2E_ARGS+=("--skip_cleanup")
E2E_ARGS+=("--use_local_cluster")

export HUB=${HUB:-"gcr.io/istio-testing"}
export TAG="${TAG:-${GIT_SHA}}"

docker images "${HUB}"/*:"${TAG}" -q | xargs docker rmi -f

make init
make docker

function build_kind_images(){
	# Create a temp directory to store the archived images.
	TMP_DIR=$(mktemp -d)
	IMAGE_FILE="${TMP_DIR}"/image.tar 

	# Archived local images and load it into KinD's docker daemon
	# Kubernetes in KinD can only access local images from its docker daemon.
	docker images "${HUB}"/*:"${TAG}"| awk 'FNR>1 {print $1 ":" $2}' | xargs docker save -o "${IMAGE_FILE}"
	kind load --name e2e-suite image-archive "${IMAGE_FILE}"

	# Delete the local tar images.
	rm -rf "${IMAGE_FILE}"
}


# getopts only handles single character flags
for ((i=1; i<=$#; i++)); do
    case ${!i} in
        # -s/--single_test to specify test target to run.
        # e.g. "-s e2e_mixer" will trigger e2e mixer_test
        -s|--single_test) ((i++)); SINGLE_TEST=${!i}
        continue
        ;;
        --timeout) ((i++)); E2E_TIMEOUT=${!i}
        continue
        ;;
    esac
    E2E_ARGS+=( "${!i}" )
done

time ISTIO_DOCKER_HUB=$HUB \
  E2E_ARGS="${E2E_ARGS[*]}" \
  JUNIT_E2E_XML="${ARTIFACTS_DIR}/junit.xml" \
  make with_junit_report TARGET="${SINGLE_TEST}" ${E2E_TIMEOUT:+ E2E_TIMEOUT="${E2E_TIMEOUT}"}