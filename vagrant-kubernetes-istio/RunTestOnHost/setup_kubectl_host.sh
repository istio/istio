# Setting up kubectl on host to talk to kubernetest cluster on Vagrant VM.
echo "your old ~/.kube/config file can be found at ~/.kube/config_old"
cp ~/.kube/config ~/.kube/config_old
vagrant ssh -c "cp /etc/kubeconfig.yml ~/.kube/config"
vagrant ssh -c "cat ~/.kube/config" > ~/.kube/config
sed -i '/Connection to 127.0.0.1 closed/d' ~/.kube/config
