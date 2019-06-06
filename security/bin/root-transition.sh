#!/bin/bash

trustdomain() {
  openssl x509 -in ${1} -noout -issuer | cut -f3 -d'='
}

check_secret () {
	MD5=`kubectl get secret $1 -o yaml -n $2 | sed -n 's/^.*root-cert.pem: //p' | md5sum | awk '{print $1}'`
	if [ "$ROOT_CERT_MD5" != "$MD5" ]; then
		echo "  Secret $2.$1 is NOT updated."
		NOT_UPDATED="$NOT_UPDATED $2.$1"
	else
		echo "  Secret $2.$1 is updated."
	fi
}

verify_namespace () {
	SECRETS=`kubectl get secret -n $1 | grep "istio\.io\/key-and-cert" | awk '{print $1}'`
	for s in $SECRETS
	do
		check_secret $s $1
	done
}

verify() {
  NOT_UPDATED=

  echo "This script checks the current root CA certificate is propagated to all the Istio-managed workload secrets in the cluster."

  ROOT_SECRET=`kubectl get secret istio-ca-secret -o yaml -n istio-system | sed -n 's/^.*ca-cert.pem: //p'`
  if [ -z $ROOT_SECRET ]; then
    echo "Root secret is empty. Are you using the self-signed CA?"
    exit
  fi

  ROOT_CERT_MD5=`kubectl get secret istio-ca-secret -o yaml -n istio-system | sed -n 's/^.*ca-cert.pem: //p' | md5sum | awk '{print $1}'`

  echo Root cert MD5 is $ROOT_CERT_MD5

  NS=`kubectl get ns | grep -v "STATUS" | grep -v "kube-system" | grep -v "kube-public" | awk '{print $1}'`

  for n in $NS
  do
    echo "Checking namespace: $n"
    verify_namespace $n
  done

  if [ -z $NOT_UPDATED ]; then
    echo "------All Istio keys and certificates are updated in secret!"
  else
    echo "------The following secrets are not updated: " $NOT_UPDATED
  fi
}

check() {
  ROOT_SECRET=`kubectl get secret istio-ca-secret -o yaml -n istio-system | sed -n 's/^.*ca-cert.pem: //p'`
  if [ -z $ROOT_SECRET ]; then
    echo "Root secret is empty. Are you using the self-signed CA?"
    return
  fi

  echo "Fetching root cert from istio-system namespace..."
  kubectl get secret -n istio-system istio-ca-secret -o yaml | awk '/ca-cert/ {print $2}' | base64 --decode > ca.cert
  if [[ ! -f ./ca.cert ]]; then
    echo "failed to get cacert, check the istio installation namespace."
    return
  fi

  rootDate=$(openssl x509 -in ca.cert -noout -enddate | cut -f2 -d'=')
  rootSec=$(date -d "${rootDate}" '+%s')
  nowSec=`date '+%s'`
  remainDays=$(echo "(${rootSec} - ${nowSec}) / (3600 * 24)" | bc)

  cat << EOF
Your Root Cert will expire after
   ${rootDate}
Current time is
  $(date)


===YOU HAVE ${remainDays} DAYS BEFORE THE ROOT CERT EXPIRES!=====

EOF
}

transition() {
  # Get root cert and private key and generate a 10 year root cert:
  kubectl get secret istio-ca-secret -n istio-system -o yaml | sed -n 's/^.*ca-cert.pem: //p' | base64 --decode > old-ca-cert.pem
  kubectl get secret istio-ca-secret -n istio-system -o yaml | sed -n 's/^.*ca-key.pem: //p' | base64 --decode > ca-key.pem

  TRUST_DOMAIN="$(echo -e "$(trustdomain old-ca-cert.pem)" | sed -e 's/^[[:space:]]*//')"
  echo "Create new ca cert, with trust domain as $TRUST_DOMAIN"
  openssl req -x509 -new -nodes -key ca-key.pem -sha256 -days 3650 -out new-ca-cert.pem -subj "/O=${TRUST_DOMAIN}"

  echo "$(date) delete old CA secret"
  kubectl -n istio-system delete secret istio-ca-secret
  echo "$(date) create new CA secret"
  kubectl create -n istio-system secret generic istio-ca-secret --from-file=ca-key.pem=ca-key.pem --from-file=ca-cert.pem=new-ca-cert.pem --type=istio.io/ca-root

  echo "$(date) Restarting Citadel ..."
  kubectl delete pod -l istio=citadel -n istio-system

  echo "$(date) restarted Citadel, checking status"
  kubectl get pods -l istio=citadel -n istio-system

  echo "New root certificate:"
  openssl x509 -in new-ca-cert.pem -noout -text

  echo "Your old certificate is stored as old-ca-cert.pem, and your private key is stored as ca-key.pem"
  echo "Please save them safely and privately."
}

OPERRATION=${1:-check}

case $1 in
  check)
    check
    ;;

  transition)
    transition
    ;;

  verify)
    verify
    ;;

  *)
    echo $"Usage: $0: check | transition | verify "
esac
