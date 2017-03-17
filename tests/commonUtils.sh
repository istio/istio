#!/bin/bash

# Copyright 2017 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

print_block_echo(){
    echo ""
    echo "#################################"
    echo $1
    echo "#################################"
    echo ""
}

compare_output() {
    EXPECTED=$1
    RECEIVED=$2
    USER=$3
    
    # Sort JSON fields, or fallback to original text
    file1=$(jq -S . $EXPECTED 2> /dev/null) || file1=$(cat $EXPECTED)
    file2=$(jq -S . $RECEIVED 2> /dev/null) || file2=$(cat $RECEIVED)

    # Diff, but ignore all whitespace
    diff -Ewb <(echo $file1) <(echo $file2) #&>/dev/null
    if [ $? -gt 0 ]
    then   
        echo "Received product page does not match $EXPECTED for user=$USER"
        return 1
    else
        echo "Received product page matches $EXPECTED for user=$USER"
        return 0
    fi
}

modify_rules_namespace(){
    print_block_echo "Modifying rules to match namespace"
    find $SCRIPTDIR/apps/bookinfo/rules/ -type f -print0 | xargs -0 sed -i "s/_CHANGEME_/$NAMESPACE/g"
}

# Call the specified endpoint and compare against expected output
# Ensure the % falls within the expected range
check_routing_rules() {
    COMMAND_INPUT="$1"
    EXPECTED_OUTPUT1="$2"
    EXPECTED_OUTPUT2="$3"
    EXPECTED_PERCENT="$4"
    MAX_LOOP=5
    routing_retry_count=1
    COMMAND_INPUT="${COMMAND_INPUT} >/tmp/routing.tmp"

    while [  $routing_retry_count -le $((MAX_LOOP)) ]; do
        v1_count=0
        v3_count=0
        for count in {1..100}
        do
            temp_var1=$(eval $COMMAND_INPUT)
            compare_output $EXPECTED_OUTPUT1 "/tmp/routing.tmp" "test-user" &>/dev/null
            if [ $? -eq 0 ]; then
                (( v1_count=v1_count+1 ))
            else
                compare_output $EXPECTED_OUTPUT2 "/tmp/routing.tmp" "test-user" &>/dev/null
                if [ $? -eq 0 ]; then
                    (( v3_count=v3_count+1 ))
                fi
            fi
        done
        echo "    v1 was hit: "$v1_count" times"
        echo "    v3 was hit: "$v3_count" times"
        echo ""

        EXPECTED_V1_PERCENT=$((100-$EXPECTED_PERCENT))
        ADJUST=5
        if [ $v1_count -lt $(($EXPECTED_V1_PERCENT-$ADJUST)) ] || [  $v1_count -gt $(($EXPECTED_V1_PERCENT+$ADJUST)) ]; then
            echo "  The routing did not meet the rule that was set, try again."
            (( routing_retry_count=routing_retry_count+1 ))
        else
            # Test passed, time to exit the loop
            routing_retry_count=100
        fi

        if [ $routing_retry_count -eq $((MAX_LOOP+1)) ]; then
            echo "Test failed"
            echo ""
            return 1
        elif [ $routing_retry_count -eq 100 ]; then
            echo "Passed test"
            echo ""
        fi
    done
    return 0
}