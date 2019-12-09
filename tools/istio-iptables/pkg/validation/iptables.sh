# This script is the helper script to simulate an istio iptables setup on local machine.
sudo iptables -t nat -N ISTIO_IN_REDIRECT
sudo iptables -t nat -I ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-port 15002
sudo iptables -t nat -I PREROUTING -p tcp -m tcp -d 127.0.0.6  -j ISTIO_IN_REDIRECT
sudo iptables -t nat -I OUTPUT  -d 127.0.0.6 -o lo -j ISTIO_IN_REDIRECT

ip -6 addr add 6::1/64 dev lo
sudo ip6tables -t nat -N ISTIO_IN_REDIRECT
sudo ip6tables -t nat -I ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-port 15002
sudo ip6tables -t nat -I PREROUTING -p tcp -m tcp -d ::6  -j ISTIO_IN_REDIRECT
sudo ip6tables -t nat -I OUTPUT  -d ::6 -o lo -j ISTIO_IN_REDIRECT
