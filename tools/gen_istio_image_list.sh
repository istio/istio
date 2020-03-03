#!/bin/bash
# 
# Copyright 2019 Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This script generates the list of images used by Istio on the control
# plane, data plane as well as addons.We generate this list by looking
# at the values.yaml file where the Helm charts are stored, as well as
# reading the values of DOCKER_TARGETS rule in the istio-docker.mk file.

ISTIO_HELM_CHART=https://api.github.com/repos/istio/istio/contents/install/kubernetes/helm/istio/charts
ISTIO_GITHUB=https://raw.githubusercontent.com/istio/istio

function get_istio_images() {
    buff=''
    continue_processing=0

    curl --silent "${ISTIO_GITHUB}/${1}/tools/istio-docker.mk" -o temp.file

    while IFS= read -r line 
    do 
       if [[ $line == DOCKER_TARGETS* ]]
       then
           buff+=${line#DOCKER_TARGETS}
           continue_processing=1
       else
           if [ $continue_processing -eq 1 ]
           then
              if [[ $line == *\\ ]]
              then
                buff=${buff%\\}$line
              else
                buff=${buff%\\}$line
                break
              fi
           fi
       fi
    done < temp.file

    for repo in $buff; do
       if [[ $repo == *docker.* ]]
       then
           istio_component=${repo##*docker.};
           istio_repo=istio/$istio_component;
           echo docker.io/"$istio_repo":"${1}"
       fi
    done

    if [ -f temp.file ];
    then
       rm temp.file
    fi
}

function get_current_release() {
    TAGS="$(curl --silent 'https://api.github.com/repos/istio/istio/tags' | grep -o '"name": "[0-9].[0-9].[0-9]"' | tr -d '"' )"
    for tag in $TAGS;
    do
      if [[ $tag != name: ]]
      then
         if [[ $tag > $latest_tag ]]
         then
            latest_tag=$tag
         fi
      fi
    done
    echo "$latest_tag"
}

function get_istio_addons() {
    branches=$(curl --silent "${ISTIO_HELM_CHART}?ref=${1}" | grep '"name": *' | tr -d '", ')
    for branch in ${branches}; do
        REPO=${branch##name:}
        curl --silent "${ISTIO_GITHUB}/${1}/install/kubernetes/helm/istio/charts/${REPO}/values.yaml" -o temp.file
        
        HUB=$(grep -E 'hub: |repository: ' temp.file | awk '{print $2 $4}')
        IMAGE=$(grep "image: " temp.file | awk '{print $2 $4}')
        TAG=$(grep "tag: " temp.file | awk '{print $2 $4}')

        hubs=()
        images=()
        tags=()

        for hub in ${HUB}; do
          hubs+=( "$hub" );
        done

        for img in ${IMAGE}; do
          images+=( "$img" );
        done

        for tag in ${TAG}; do
          tags+=( "$tag" );
        done

        for i in "${!hubs[@]}"; do
          if [ -z "${images[$i]}" ]
          then
             echo "${hubs[$i]}":"${tags[$i]}"
          else
             echo "${hubs[$i]}"/"${images[$i]}":"${tags[$i]}"
          fi 
        done
    done
    if [ -f temp.file  ];
    then 
       rm temp.file
    fi
}

function display_usage() {
    echo 
    echo "List Istio images and addons for a given release. If release is not specified, it will list images for the latest Istio release."
    echo 
    echo "USAGE: ./gen_istio_image_list.sh [-v|--version] [-h|--help]"
    echo "      -v|--version : Istio release"
    echo "      -h|--help : Prints usage information"
    exit 1
}

# Process the input arguments.
while [[ "$#" -gt 0 ]];
do
   case "$1" in
      -v|--version )
          if [ -z "$2"  ];
          then
             echo "Please provide an Istio release."
             display_usage
          else
             if [[ "$2" =~ [0-9].[0-9].[0-9] ]];
             then
                CURRENT_RELEASE="$2"
             else
                echo
                echo "Enter a valid Istio release [<major>.<minor>.<LTS patch level>]"
                echo
                exit 1
             fi
          fi
          shift 2;;
      -h|--help )
          display_usage ;;
      * )
          echo "Unknown argument: $1"
          display_usage ;;
   esac
done

if [ -z "${CURRENT_RELEASE}" ]
then
    CURRENT_RELEASE=$(get_current_release)
fi
get_istio_images "${CURRENT_RELEASE}"
get_istio_addons "${CURRENT_RELEASE}"
