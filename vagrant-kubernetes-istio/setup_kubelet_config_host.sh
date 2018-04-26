# Setting up kubectl on host to talk to kubernetest cluster on Vagrant VM.
echo "your old ~/.kube/config file can be found at ~/.kube/config_old"
ls ~/.kube/config_old
if [ $? -ne 0 ]; then
    cp ~/.kube/config ~/.kube/config_old
fi
vagrant ssh -c "cat ~/.kube/config" > ~/.kube/config
