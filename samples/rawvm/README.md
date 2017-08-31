
# RawVM in Istio 0.2 demo notes

## MySQL Installation:

### Official oracle version
```shell
wget https://dev.mysql.com/get/mysql-apt-config_0.8.7-1_all.deb
sudo dpkg -i mysql-apt-config_0.8.7-1_all.deb
# Select server 5.7 (default), tools and previews not needed/disabled
sudo apt-get update
sudo apt-get install mysql-server
# Select no remote password

# Create tables :
sudo mysql < ~/github/istio/samples/apps/bookinfo/src/mysql/mysqldb-init.sql
```

### Or
sudo apt-get mariadb-server

## Sidecar
See
https://github.com/istio/proxy/tree/master/tools/deb
