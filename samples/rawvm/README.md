# RawVM in Istio 0.2 demo notes

## MySQL Installation

### Official oracle version

```shell
wget https://dev.mysql.com/get/mysql-apt-config_0.8.7-1_all.deb
sudo dpkg -i mysql-apt-config_0.8.7-1_all.deb
# Select server 5.7 (default), tools and previews not needed/disabled
sudo apt-get update
sudo apt-get install mysql-server
# Clearly this is insecure, don't do that for prod !
sudo mysql
   ALTER USER 'root'@'localhost' IDENTIFIED WITH mysql_native_password BY 'password';
   # Remote 'root' can read test.*, to avoid
   # ERROR 1130 (HY000): Host '...' is not allowed to connect to this MySQL server
   create user 'root'@'%' IDENTIFIED WITH mysql_native_password BY 'password';
   GRANT SELECT ON test.* TO 'root'@'%';
# Create tables :
mysql -u root -h 127.0.0.1 --password=password < ~/github/istio/samples/bookinfo/src/mysql/mysqldb-init.sql
# And to be able to connect remotely (only needed to test before injection)
sudo vi /etc/mysql/mysql.conf.d/mysqld.cnf
# comment out:
#bind-address   = 127.0.0.1
sudo service mysql restart
# check it's now binding on *
$ sudo lsof -i :3306
COMMAND   PID  USER   FD   TYPE DEVICE SIZE/OFF NODE NAME
mysqld  29145 mysql   31u  IPv6 168376      0t0  TCP *:mysql (LISTEN)
# from another host, verify it works:
ldemailly@benchmark-2:~$ mysql -u root -h instance-1 --password=password test -e "select * from ratings"
mysql: [Warning] Using a password on the command line interface can be insecure.
+----------+--------+
| ReviewID | Rating |
+----------+--------+
|        1 |      5 |
|        2 |      6 |
+----------+--------+
# for low volume troubleshooting:
mysql -u root -h instance-1 --password=password
  SET global general_log_file='/tmp/mysqlquery.log';
  SET global general_log = 1;
# then
tail -f /tmp/mysqlquery.log
```

### Or

sudo apt-get mariadb-server

TODO: figure out equivalent of above for mariadb

<https://stackoverflow.com/questions/28068155/access-denied-for-user-rootlocalhost-using-password-yes-after-new-instal>

## Sidecar

See <https://github.com/istio/proxy/tree/master/tools/deb>

## Bookinfo with MySql in k8s

You need 5 nodes in your cluster to add mysql (until we tune the requests)

```bash
# source istio.VERSION
wget https://storage.googleapis.com/istio-artifacts/pilot/$PILOT_TAG/artifacts/istioctl/istioctl-osx
chmod 755 istioctl-osx
./istioctl-osx kube-inject --hub $PILOT_HUB --tag $PILOT_TAG -f samples/bookinfo/kube/bookinfo.yaml > bookinfo-istio.yaml
kubectl apply -f bookinfo-istio.yaml
./istioctl-osx kube-inject --hub $PILOT_HUB --tag $PILOT_TAG -f samples/bookinfo/kube/bookinfo-mysql.yaml > bookinfo-mysql-istio.yaml
kubectl apply -f bookinfo-mysql-istio.yaml
./istioctl-osx kube-inject --hub $PILOT_HUB --tag $PILOT_TAG -f samples/bookinfo/kube/bookinfo-ratings-v2.yaml > bookinfo-ratings-v2-istio.yaml
kubectl apply -f bookinfo-ratings-v2-istio.yaml
# use it (ratings v2 and mysql)
kubectl apply -f samples/bookinfo/networking/virtual-service-ratings-mysql.yaml
# wait a bit / reload product page
# see mysql in grafana and 5,6 stars
kubectl port-forward mysqldb-v1-325529163-9x1r0 3306:3306 # use actual mysql pod
mysql -u root -h 127.0.0.1 --password=password test
  select * from ratings;
  update ratings set rating=3 where reviewid=1;
# see first rating change to 3 stars

# for metrics:
fortio load -t 1m http://$INGRESS_IP/productpage
```

## Move MySQL to VM

1. remove the k8s based service

    ```bash
    kubectl delete svc mysqldb
    ```

1. observe `product ratings not available` when re-loading the page

1. register the VM instead:

    ```bash
    ./istioctl-osx register mysqldb 10.138.0.13 3306
    I0904 11:12:56.785430   34562 register.go:44] Registering for service 'mysqldb' ip '10.138.0.13', ports list [{3306 mysql}]
    I0904 11:12:56.785536   34562 register.go:49] 0 labels ([]) and 1 annotations ([alpha.istio.io/kubernetes-serviceaccounts=default])
    W0904 11:12:56.887017   34562 register.go:123] Got 'services "mysqldb" not found' looking up svc 'mysqldb' in namespace 'default', attempting to create it
    W0904 11:12:56.938721   34562 register.go:139] Got 'endpoints "mysqldb" not found' looking up endpoints for 'mysqldb' in namespace 'default', attempting to create them
    I0904 11:12:57.055643   34562 register.go:180] No pre existing exact matching ports list found, created new subset {[{10.138.0.13  <nil> nil}] [] [{mysql 3306 }]}
    I0904 11:12:57.090739   34562 register.go:191] Successfully updated mysqldb, now with 1 endpoints
    ```

1. check the registration:

    ```
    kubectl get svc mysqldb -o yaml
    apiVersion: v1
    kind: Service
    metadata:
      annotations:
        alpha.istio.io/kubernetes-serviceaccounts: default
      creationTimestamp: 2017-09-04T18:12:56Z
      name: mysqldb
      namespace: default
      resourceVersion: "464459"
      selfLink: /api/v1/namespaces/default/services/mysqldb
      uid: ad746e4c-919c-11e7-9a62-42010a8a004e
    spec:
      clusterIP: 10.31.253.143
      ports:
      - name: mysql
        port: 3306
        protocol: TCP
        targetPort: 3306
      sessionAffinity: None
      type: ClusterIP
    status:
      loadBalancer: {}
    ```

## Build debian packages

Prereq:

ps: for docker - remember to "docker ps" and it should work/not error out and not require sudo, if it doesn't work add your username to /etc/group docker

For gcloud (<https://cloud.google.com/sdk/docs/quickstart-debian-ubuntu>):

```shell
export CLOUD_SDK_REPO="cloud-sdk-$(lsb_release -c -s)"
echo "deb http://packages.cloud.google.com/apt $CLOUD_SDK_REPO main" | sudo tee -a /etc/apt/sources.list.d/google-cloud-sdk.list
curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | sudo apt-key add -
sudo apt-get update && sudo apt-get install google-cloud-sdk
gcloud init
sudo apt-get install kubectl
gcloud container clusters get-credentials demo-1 --zone us-west1-b --project istio-demo-0-2
```

Note to install rbac yaml you need:

```bash
kubectl create clusterrolebinding my-admin-access --clusterrole cluster-admin --user USERNAME
```

Then:

```bash
git clone https://github.com/istio/proxy.git -b rawvm-demo-0-2-2
tools/deb/test/build_all.sh
```
