#!/bin/bash -e

AddDataSource() {
  curl 'http://localhost:3000/api/datasources' \
    -X POST \
    -uadmin:admin\
    -H 'Content-Type: application/json;charset=UTF-8' \
    --data-binary \
    '{"name":"Prometheus","type":"prometheus","url":"http://prometheus:9090","access":"proxy","isDefault":true}'
}


# dashboard exported thru grafana UI contains datasource variable
# in the __input section 
# DS_PROMETHEUS is replaced by "Prometheus" datasource 
# that was created by AddDataSource().
ImportDashboard() {
  if [[ ! -z ${DASHBOARD_URL} ]];then
    curl ${DASHBOARD_URL} -o  /grafana-dashboard.json
  fi
  export FL="/tmp/dash"

  echo "{ \"overwrite\": true, \"dashboard\": " > $FL
  sed 's/${DS_PROMETHEUS}/Prometheus/g' /grafana-dashboard.json >> $FL
  echo "}" >> $FL

  curl -d @$FL -X POST http://localhost:3000/api/dashboards/db   -H 'Content-Type: application/json;charset=UTF-8' -uadmin:admin
}

# give time for grafana to start
sleep 10
echo 'Importing Grafana Dashboard'

until AddDataSource; do
  echo 'Configuring Grafana...'
  sleep 2
done

ImportDashboard
