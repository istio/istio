#!/bin/bash
set -ev
if [ "${TRAVIS_PULL_REQUEST}" = "false" ]; then
	openssl aes-256-cbc -K $encrypted_2f660428f0db_key -iv $encrypted_2f660428f0db_iv -in istio-test.json.enc -out istio-test.json -d

	gcloud auth activate-service-account travis-ci-creator@istio-test.iam.gserviceaccount.com --key-file=istio-test.json

	docker/gcloud_build.sh
fi
