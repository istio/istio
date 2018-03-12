#!/bin/sh
## Copies the flexvolume driver to the desired location on the host system
## TODO(wattli): considering move this driver to node agent.

cp /usr/local/bin/flexvolume /host/driver/.uds
chmod 0550 /host/driver/.uds
mv /host/driver/.uds /host/driver/uds
