# Notes on minikube none 

On startup it'll run:

```
CONTAINER ID        IMAGE                                                 COMMAND                  CREATED             STATUS              PORTS               NAMES
76ba6a7c355d        gcr.io/google_containers/kubernetes-dashboard-amd64   "/dashboard --inse..."   2 minutes ago       Up 2 minutes                            k8s_kubernetes-dashboard_kubernetes-dashboard-ct5tj_kube-system_077b75ce-e8f0-11e7-addf-42010a8e01b2_0
155fa8e944d9        gcr.io/google_containers/pause-amd64:3.0              "/pause"                 2 minutes ago       Up 2 minutes                            k8s_POD_kubernetes-dashboard-ct5tj_kube-system_077b75ce-e8f0-11e7-addf-42010a8e01b2_0

d38bff3e95b8        fed89e8b4248                                          "/sidecar --v=2 --..."   2 minutes ago       Up 2 minutes                            k8s_sidecar_kube-dns-1326421443-1jrw2_kube-system_07b7817d-e8f0-11e7-addf-42010a8e01b2_0
90836a6b709c        459944ce8cc4                                          "/dnsmasq-nanny -v..."   2 minutes ago       Up 2 minutes                            k8s_dnsmasq_kube-dns-1326421443-1jrw2_kube-system_07b7817d-e8f0-11e7-addf-42010a8e01b2_0
d2761c06deb3        512cd7425a73                                          "/kube-dns --domai..."   2 minutes ago       Up 2 minutes                            k8s_kubedns_kube-dns-1326421443-1jrw2_kube-system_07b7817d-e8f0-11e7-addf-42010a8e01b2_0
49456b37bb93        gcr.io/google_containers/pause-amd64:3.0              "/pause"                 2 minutes ago       Up 2 minutes                            k8s_POD_kube-dns-1326421443-1jrw2_kube-system_07b7817d-e8f0-11e7-addf-42010a8e01b2_0

2cbe94aba6b5        gcr.io/k8s-minikube/storage-provisioner               "/storage-provisioner"   2 minutes ago       Up 2 minutes                            k8s_storage-provisioner_storage-provisioner_kube-system_068a4b54-e8f0-11e7-addf-42010a8e01b2_0
f93f12e7d8ae        gcr.io/google_containers/pause-amd64:3.0              "/pause"                 2 minutes ago       Up 2 minutes                            k8s_POD_storage-provisioner_kube-system_068a4b54-e8f0-11e7-addf-42010a8e01b2_0

81c5d87ff62f        0a951668696f                                          "/opt/kube-addons.sh"    2 minutes ago       Up 2 minutes                            k8s_kube-addon-manager_kube-addon-manager-prealloc-yrdszvli-0df71a5c-b7e1-4416-a67b-480e567d6a3f_kube-system_5b0c47ee629af3668fe115f798f73437_0
33e7bd71d62d        gcr.io/google_containers/pause-amd64:3.0              "/pause"                 2 minutes ago       Up 2 minutes                            k8s_POD_kube-addon-manager-prealloc-yrdszvli-0df71a5c-b7e1-4416-a67b-480e567d6a3f_kube-system_5b0c47ee629af3668fe115f798f73437_0


```

```
REPOSITORY                                             TAG                 IMAGE ID            CREATED             SIZE
gcr.io/google_containers/kubernetes-dashboard-amd64    v1.8.0              55dbc28356f2        3 weeks ago         119MB
gcr.io/k8s-minikube/storage-provisioner                v1.8.0              4689081edb10        6 weeks ago         80.8MB
gcr.io/k8s-minikube/storage-provisioner                v1.8.1              4689081edb10        6 weeks ago         80.8MB
gcr.io/google_containers/k8s-dns-sidecar-amd64         1.14.5              fed89e8b4248        2 months ago        41.8MB
gcr.io/google_containers/k8s-dns-kube-dns-amd64        1.14.5              512cd7425a73        2 months ago        49.4MB
gcr.io/google_containers/k8s-dns-dnsmasq-nanny-amd64   1.14.5              459944ce8cc4        2 months ago        41.4MB
gcr.io/google_containers/kubernetes-dashboard-amd64    v1.6.3              691a82db1ecd        4 months ago        139MB
gcr.io/google-containers/kube-addon-manager            v6.4-beta.2         0a951668696f        6 months ago        79.2MB
gcr.io/google_containers/pause-amd64                   3.0                 99e59f495ffa        19 months ago       747kB


```

Apiserver:

```bash

/usr/local/bin/localkube --dns-domain=cluster.local 
   --extra-config=apiserver.Admission.PluginNames=Initializers,NamespaceLifecycle,LimitRanger,ServiceAccount,DefaultStorageClass,GenericAdmissionWebhook,ResourceQuota 
  --generate-certs=false --logtostderr=true --enable-dns=false

```

# Networking

cat /etc/resolv.conf  

nameserver 10.0.0.10
search istio-system.svc.cluster.local svc.cluster.local cluster.local c.circleci-picard-mahmood.internal google.internal
options ndots:5

ip (on docker):
172.17.0.4/16 


```bash
NAMESPACE      NAME                       CLUSTER-IP   EXTERNAL-IP   PORT(S)                                                            AGE
default        svc/fortio-noistio         10.0.0.170   <none>        8080/TCP,8079/TCP                                                  28m
default        svc/kubernetes             10.0.0.1     <none>        443/TCP                                                            44m
istio-system   svc/echosrv                10.0.0.189   <none>        8080/TCP,8079/TCP                                                  28m
istio-system   svc/fortio-noistio         10.0.0.196   <none>        8080/TCP,8079/TCP                                                  28m
istio-system   svc/istio-ingress          10.0.0.49    <nodes>       80:32013/TCP,443:30191/TCP                                         29m
istio-system   svc/istio-mixer            10.0.0.166   <none>        9091/TCP,15004/TCP,9093/TCP,9094/TCP,9102/TCP,9125/UDP,42422/TCP   29m
istio-system   svc/istio-pilot            10.0.0.111   <none>        15003/TCP,443/TCP                                                  29m
istio-system   svc/prometheus             10.0.0.119   <none>        9090/TCP                                                           28m
istio-system   svc/zipkin                 10.0.0.24    <none>        9411/TCP                                                           28m
kube-system    svc/kubernetes-dashboard   10.0.0.122   <nodes>       80:30000/TCP                                                       44m


```

Kubedns on 172.17.0.3/16

cat /etc/resolv.conf 
nameserver 169.254.169.254

---
apiVersion: v1
kind: Pod
metadata:
  name: busybox1
  labels:
    name: busybox
spec:
  hostname: busybox-1
  subdomain: default-subdomain
  containers:
  - image: busybox
    command:
      - sleep
      - "3600"
    name: busybox

nslookup istio-pilot.istio-system 172.17.0.3:53
-> works

 kubectl get pods --namespace=kube-system -l k8s-app=kube-dns
-> works












