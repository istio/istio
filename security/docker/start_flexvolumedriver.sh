#!/bin/sh

cp /usr/local/bin/flexvolumedriver /host/driver/.uds
chmod 0550 /host/driver/.uds
mv /host/driver/.uds /host/driver/uds
