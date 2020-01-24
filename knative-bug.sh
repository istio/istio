function prepare() {
kubectl create ns bug
kubectl create ns bug-alt
kubectl label ns bug istio-injection=enabled 
kubectl label ns bug-alt istio-injection=enabled

cat <<EOF | kubectl apply -f -
apiVersion: "authentication.istio.io/v1alpha1"
kind: "Policy"
metadata:
  name: "default"
  namespace: "bug"
spec:
  peers:
  - mtls:
      mode: STRICT
---
apiVersion: "authentication.istio.io/v1alpha1"
kind: "Policy"
metadata:
  name: "default"
  namespace: "bug-alt"
spec:
  peers:
  - mtls:
      mode: PERMISSIVE
---
apiVersion: "networking.istio.io/v1alpha3"
kind: "DestinationRule"
metadata:
  name: "mtls-services"
  namespace: "bug"
spec:
  host: "*.local"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
EOF

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: sleep
  namespace: bug
---
apiVersion: v1
kind: Service
metadata:
  name: sleep
  namespace: bug
  labels:
    app: sleep
spec:
  ports:
  - port: 80
    name: http
  selector:
    app: sleep
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: sleep
  namespace: bug
spec:
  replicas: 1
  selector:
    matchLabels:
      app: sleep
  template:
    metadata:
      labels:
        app: sleep
      annotations:
        sidecar.istio.io/inject: "true"
    spec:
      serviceAccountName: sleep
      containers:
      - name: sleep
        image: governmentpaas/curl-ssl
        command: ["/bin/sleep", "3650d"]
        imagePullPolicy: IfNotPresent
        volumeMounts:
        - mountPath: /etc/sleep/tls
          name: secret-volume
      volumes:
      - name: secret-volume
        secret:
          secretName: sleep-secret
          optional: true
EOF

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Service
metadata:
  name: httpbin
  namespace: bug
  labels:
    app: httpbin
spec:
  ports:
  - name: http
    port: 8000
    targetPort: 80
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: httpbin1
  namespace: bug
spec:
  replicas: 1
  selector:
    matchLabels:
      app: httpbin1
      version: v1
  template:
    metadata:
      labels:
        app: httpbin1
        version: v1
      annotations:
        sidecar.istio.io/inject: "true"
    spec:
      containers:
      - image: docker.io/kennethreitz/httpbin
        imagePullPolicy: IfNotPresent
        name: httpbin
        ports:
        - containerPort: 80

---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: httpbin2
  namespace: bug-alt
spec:
  replicas: 1
  selector:
    matchLabels:
      app: httpbin2
      version: v1
  template:
    metadata:
      labels:
        app: httpbin2
        version: v1
      annotations:
        sidecar.istio.io/inject: "true"
    spec:
      containers:
      - image: docker.io/kennethreitz/httpbin
        imagePullPolicy: IfNotPresent
        name: httpbin
        ports:
        - containerPort: 80
EOF
}

function endpoint1() {
  name=${1:-httpbin1}
  podIP=$(kubectl -n bug get pod -l app=${name} -o jsonpath='{.items[*].status.podIP}')
  podName=$(kpidn bug -l app=${name})
  echo "name ${name}, podIP ${podIP}, podName ${podName}"
kubectl -n bug apply -f - <<EOF
apiVersion: v1
kind: Endpoints
metadata:
  name: httpbin
  namespace: bug
subsets:
  - addresses:
    - ip: ${podIP} ### Replace your httpbin1 pod's IP
      targetRef:
        kind: Pod
        name: ${podName} ### Replace your httpbin1 pod's name
        namespace: bug
    ports:
    - name: http
      port: 80
      protocol: TCP
EOF
}

function endpoint2() {
  name=${1:-httpbin2}
  podIP=$(kubectl -n bug-alt get pod -l app=${name} -o jsonpath='{.items[*].status.podIP}')
  podName=$(kpidn bug-alt -l app=${name})
  echo "name ${name}, podIP ${podIP}, podName ${podName}"
kubectl -n bug apply -f - <<EOF
apiVersion: v1
kind: Endpoints
metadata:
  name: httpbin
  namespace: bug
subsets:
  - addresses:
    - ip: ${podIP} ### Replace your httpbin1 pod's IP
      targetRef:
        kind: Pod
        name: ${podName} ### Replace your httpbin1 pod's name
        namespace: bug-alt
    ports:
    - name: http
      port: 80
      protocol: TCP
EOF
}

function addse() {
ip1=$(kubectl -n bug get pod -l app=httpbin1 -o jsonpath='{.items[*].status.podIP}')
ip2=$(kubectl -n bug-alt get pod -l app=httpbin2 -o jsonpath='{.items[*].status.podIP}')
kubectl -n bug apply -f - <<EOF
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: httpbin2-se-update
spec:
  hosts:
  - httpbin.bug.svc
  location: MESH_INTERNAL
  ports:
  - number: 8000
    name: http1
    protocol: http
  resolution: STATIC
  endpoints:
  - address: ${ip1}
    ports:
      http1: 80
  - address: ${ip2}
    ports:
      http1: 80
EOF
}

function check() {
  podName=$(kpidn bug -l app=sleep)
  kubectl -n bug exec -it ${podName} -- curl httpbin.bug.svc:8000/ip
}


