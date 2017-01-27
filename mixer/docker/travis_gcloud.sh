#!/bin/bash
set -ev

export PROJECT="istio-testing"

if [ "${TRAVIS_PULL_REQUEST}" = "false" ]; then
	openssl aes-256-cbc -K $encrypted_2f660428f0db_key -iv $encrypted_2f660428f0db_iv -in $PROJECT.json.enc -out $PROJECT.json -d

	gcloud auth activate-service-account travis-ci-creator@$PROJECT.iam.gserviceaccount.com --key-file=$PROJECT.json

	docker/gcloud_build.sh
fi
