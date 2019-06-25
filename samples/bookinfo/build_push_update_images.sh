#!/bin/bash
#
# Copyright 2018 Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

set -o errexit

display_usage() {
    echo
    echo "USAGE: ./build_push_update_images.sh <version> [-h|--help] [--scan-images]"
    echo "	version : Version of the sample app images (Required)"
    echo "	-h|--help : Prints usage information"
    echo -e "	--scan-images : Enable security vulnerability scans for docker images \n\t\t\trelated to bookinfo sample apps. By default, this feature \n\t\t\tis disabled."
    exit 1
}

# Check if there is atleast one input argument
if [[ -z "$1" ]] ; then
	echo "Missing version parameter"
        display_usage
else
	VERSION="$1"
fi

# Process the input arguments. By default, image scanning is disabled. 
index=0
ENABLE_IMAGE_SCAN=false
for i in "$@"
do
	case "$i" in
		"--scan-images" )
		   ENABLE_IMAGE_SCAN=true ;;
		-h|--help )
                   echo
		   echo "Build the docker images for bookinfo sample apps, push them to docker hub and update the yaml files."
		   display_usage ;;
                * )
                   if [[ $index -ne 0 ]]; then
                     echo "Unknown argument: $i"
                     display_usage 
		   fi;;
	esac
  	((index++))
done

#Build docker images
src/build-services.sh "${VERSION}"

#get all the new image names and tags
for v in ${VERSION} "latest"
do
  IMAGES+=$(docker images -f reference=istio/examples-bookinfo*:"$v" --format "{{.Repository}}:$v")
  IMAGES+=" "
done

#
# Run security vulnerability scanning on bookinfo sample app images using
# the ImageScanner tool. If the reuqest is handled successfully, it gives
# the output in JSON format which has the following format:
#   {
#	"Progress": "Scan completed: OK",
#	"Results": {
#		"ID": "94be3d24-cd0b-402c-837c-99d453ec8797",
#		"Scan_Time": 1559143715,
#		"Status": "OK",
#		"Vulnerabilities": [],
#		"Configuration_Issues": []
#	}
#    }
#
function run_vulnerability_scanning() {
  RESULT_DIR="vulnerability_scan_results"
  CURL_RESPONSE=$(curl -s --create-dirs -o "$RESULT_DIR/$1_$VERSION"  -w "%{http_code}" http://imagescanner.cloud.ibm.com/scan?image="docker.io/$2")
  if [ "$CURL_RESPONSE" -eq 200 ]; then
     mv "$RESULT_DIR/$1_$VERSION" "$RESULT_DIR/$1_$VERSION.json"
  fi
}

# Push images. Scan images if ENABLE_IMAGE_SCAN is true.
for IMAGE in ${IMAGES}; 
do 
  echo "Pushing: ${IMAGE}" 
  docker push "${IMAGE}"; 
  
  # $IMAGE has the following format: istio/examples-bookinfo*:"$v".
  # We want to get the sample app name from $IMAGE (the examples-bookinfo* portion)
  # to create the file to store the results of the scan for that image. The first
  # part of the $IMAGE_NAME gets examples-bookinfo*:"$v", and the second part gets
  # 'examples-bookinfo*'.
  if [[ "$ENABLE_IMAGE_SCAN" == "true"  ]]; then
  	echo "Scanning ${IMAGE} for security vulnerabilities"
  	IMAGE_NAME=${IMAGE#*/}
  	IMAGE_NAME=${IMAGE_NAME%:*}
  	run_vulnerability_scanning "${IMAGE_NAME}" "${IMAGE}"
  fi
done

#Update image references in the yaml files
find . -name "*bookinfo*.yaml" -exec sed -i.bak "s/\\(istio\\/examples-bookinfo-.*\\):[[:digit:]]*\\.[[:digit:]]*\\.[[:digit:]]*/\\1:$VERSION/g" {} +
