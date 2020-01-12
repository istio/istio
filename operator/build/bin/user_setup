#!/bin/sh
set -x

# ensure $HOME exists and is accessible by group 0 (we don't know what the runtime UID will be)
mkdir -p ${HOME}
chown ${USER_UID}:0 ${HOME}
chmod ug+rwx ${HOME}

# runtime user will need to be able to self-insert in /etc/passwd
chmod g+rw /etc/passwd

# no need for this script to remain in the image after running
rm $0
