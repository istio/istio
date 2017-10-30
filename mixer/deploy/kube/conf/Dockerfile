# starts graphana with Prometheus Datasource pre configured
# imports dashboard /grafana-dashboard.json from DASHBOARD_URL
from grafana/grafana:4.1.2

RUN apt-get update
RUN apt-get install -y curl

# allow anonymous access
ENV GF_AUTH_DISABLE_LOGIN_FORM=true
ENV GF_AUTH_ANONYMOUS_ENABLED=true
ENV GF_AUTH_ANONYMOUS_ORG_ROLE=Admin

COPY ./start.sh /start.sh
COPY ./grafana-dashboard.json /grafana-dashboard.json
COPY ./import_dashboard.sh ./import_dashboard.sh

ENTRYPOINT ["/start.sh"]
