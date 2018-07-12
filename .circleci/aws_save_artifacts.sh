#!/bin/bash

set -e

if [ "$#" -ne 1 ]; then
  echo "Usage: $0 <src dir>" >&2
  exit 1
fi

SRCDIR="$1"

if [[ $CIRCLE_BUILD_NUM == "" ]]; then
  echo "Not a circle ci build, or missing environment" >&2
  exit 1
fi

PATH_BUILD=""
PATH_LATEST=""
PATH_RELEASE=""

if [[ $CIRCLE_TAG != "" ]]; then
  echo "Saving artifacts from tag ${CIRCLE_TAG}"
  PATH_BUILD="${CIRCLE_TAG}/build-${CIRCLE_BUILD_NUM}"
  PATH_LATEST="${CIRCLE_TAG}/latest"
  PATH_RELEASE="${CIRCLE_TAG}"
elif [[ $CIRCLE_PR_NUMBER != "" ]]; then
  echo "Saving artifacts from ${CIRCLE_PR_USERNAME}'s PR ${CIRCLE_PR_NUMBER}"
  PATH_BUILD="PR/${CIRCLE_PR_NUMBER}/build-${CIRCLE_BUILD_NUM}"
  PATH_LATEST="PR/${CIRCLE_PR_NUMBER}/latest"
elif [[ $CIRCLE_BRANCH != "" ]]; then
  echo "Saving artifacts from branch ${CIRCLE_BRANCH}"
  PATH_BUILD="${CIRCLE_BRANCH}/build-${CIRCLE_BUILD_NUM}"
  PATH_LATEST="${CIRCLE_BRANCH}/latest"
else
  echo "Can't determine build type" >&2
  exit 1
fi

set +x

cat > "${SRCDIR}"/build_info.txt <<EOF
Build Log: ${CIRCLE_BUILD_URL}
Pull Request: ${CIRCLE_PULL_REQUEST}
Commit SHA1: ${CIRCLE_SHA1}
Branch: ${CIRCLE_BRANCH}
PR User: ${CIRCLE_PR_USERNAME}
EOF



aws s3 cp "${SRCDIR}" s3://aspenmesh-ci-artifacts/istio/${PATH_BUILD}/ --recursive
aws s3 sync "${SRCDIR}" s3://aspenmesh-ci-artifacts/istio/${PATH_LATEST} --delete

if [[ $PATH_RELEASE != "" ]]; then
  aws s3 cp "${SRCDIR}" s3://aspenmesh-release/${PATH_RELEASE}/ --recursive
fi
