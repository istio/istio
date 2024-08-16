#!/bin/bash

mkdir -p /var/log/proxy

mkdir -p /var/lib/istio

chown -R 1337.1337 /var/log/proxy

chown -R 1337.1337 /var/lib/logrotate

chown -R 1337.1337 /var/lib/istio

cat <<EOF > /etc/logrotate.d/higress-logrotate
/var/log/proxy/access.log
{
su 1337 1337
rotate 5
create 644 1337 1337
nocompress
notifempty
minsize 100M
postrotate
    ps aux|grep "envoy -c"|grep -v "grep"|awk  '{print $2}'|xargs -i kill -SIGUSR1 {}
endscript
}
EOF

chmod -R 0644 /etc/logrotate.d/higress-logrotate

cat <<EOF > /var/lib/istio/cron.txt
* * * * * /usr/sbin/logrotate /etc/logrotate.d/higress-logrotate
EOF
