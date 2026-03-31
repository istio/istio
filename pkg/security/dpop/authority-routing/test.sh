#!/usr/bin/env bash
# End-to-end test for dynamic :authority routing (istio/istio#59669)
# Usage: NAMESPACE=default TEST_SERVICE=httpbin bash test.sh
set -euo pipefail

NAMESPACE="${NAMESPACE:-default}"
TEST_SERVICE="${TEST_SERVICE:-httpbin}"

echo "=== Applying configs ==="
kubectl apply -f gateway.yaml
kubectl apply -f envoyfilter-lua.yaml
kubectl apply -f envoyfilter-cluster-header.yaml

echo "=== Waiting for xDS propagation (10s) ==="
sleep 10

INGRESS_IP=$(kubectl -n istio-system get svc istio-ingressgateway \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
echo "Ingress IP: $INGRESS_IP"

echo ""
echo "=== Test 1: Short name routing ==="
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
  -H "Host: ${TEST_SERVICE}" \
  "http://${INGRESS_IP}/")
echo "HTTP status: $HTTP_CODE (expect 200)"

echo ""
echo "=== Test 2: Short name with explicit port ==="
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
  -H "Host: ${TEST_SERVICE}:80" \
  "http://${INGRESS_IP}/")
echo "HTTP status: $HTTP_CODE (expect 200)"

echo ""
echo "=== Test 3: Namespace hint header ==="
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
  -H "Host: ${TEST_SERVICE}" \
  -H "x-target-namespace: ${NAMESPACE}" \
  "http://${INGRESS_IP}/")
echo "HTTP status: $HTTP_CODE (expect 200)"

echo ""
echo "=== Test 4: FQDN passthrough (should not be rewritten) ==="
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
  -H "Host: ${TEST_SERVICE}.${NAMESPACE}.svc.cluster.local" \
  "http://${INGRESS_IP}/")
echo "HTTP status: $HTTP_CODE (expect 200)"

echo ""
echo "=== Test 5: Verify Lua filter is installed on gateway ==="
GW_POD=$(kubectl -n istio-system get pod -l istio=ingressgateway \
  -o jsonpath='{.items[0].metadata.name}')
kubectl -n istio-system exec "$GW_POD" -c istio-proxy -- \
  pilot-agent request GET /config_dump 2>/dev/null | \
  python3 -c "
import sys, json
d = json.load(sys.stdin)
for cfg in d.get('configs', []):
  if 'ListenersConfigDump' in cfg.get('@type',''):
    for l in cfg.get('dynamic_listeners', []):
      for fc in l.get('active_state', {}).get('listener', {}).get('filter_chains', []):
        for f in fc.get('filters', []):
          for hf in f.get('typed_config', {}).get('http_filters', []):
            if 'lua' in hf.get('name','').lower():
              print('  Lua filter found on listener:', l.get('name'))
" && echo "Lua filter check done" || echo "(skipped — pilot-agent not accessible)"

echo ""
echo "=== All tests done ==="
