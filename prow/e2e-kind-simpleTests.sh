# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

echo 'Running Simple test with rbac, auth Tests'

./prow/e2e-kind-suite.sh --auth_enable --single_test e2e_simple --installer helm "$@"