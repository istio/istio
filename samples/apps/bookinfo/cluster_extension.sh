#!/bin/bash
#
# Copyright 2017 Istio Authors. All Rights Reserved.
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
#
################################################################################

# Script to run on a VM with VPC connectivity to the cluster, to prepare the DB and details app.


# Initialize mysql for the rating demo
function istioMysql() {
  # The ratings script is using root/password by default.

  cat <<EOF | sudo mysql
GRANT ALL PRIVILEGES on *.* to 'root'@'localhost' IDENTIFIED BY 'password';
FLUSH PRIVILEGES;
CREATE DATABASE test;
EOF

  # Create the tables
  mysql -u root -ppassword < mysql/mysqldb-init.sql
}

function istioUpdateRatings() {
  local RATING=${1-2}

  cat <<EOF | mysql -u root -ppassword
  USE test;
  update ratings set rating=$RATING where reviewid=1;
EOF
}

function istioInstallBookinfo() {
  # Configure the inbound ports
  sudo bash -c 'echo "ISTIO_INBOUND_PORTS=80,9080,3306,27017" > /var/lib/istio/envoy/sidecar.env'
  sudo chown istio-proxy /var/lib/istio/envoy/sidecar.env

  sudo apt-get install -y ruby mariadb-server mongodb

  sudo systemctl start mariadb
  sudo systemctl restart istio
  istioMysql

  startDetails
}

function startDetails() {
  # Start bookinfo components
  local app=details
  if [[ -r $app.pid ]] ; then
     kill -9 $(cat ${app}.pid)
  fi
  ruby details/details.rb 9080  > details.log 2>&1 &
  echo $! > $app.pid
}

if [[ ${1:-} == "install" ]] ; then
  istioInstallBookinfo
fi


