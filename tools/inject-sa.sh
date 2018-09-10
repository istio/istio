
#!/bin/sh

# example usage: ./inject-sa.sh
set -e  # Catch errors.

inject_ingress() {
 gcloud iam service-accounts create ${INGRESS_SA} --display-name ${INGRESS_SA}
 gcloud iam service-accounts describe ${INGRESS_SA}@${PROJECT_ID}.iam.gserviceaccount.com
 SA_ID=$(gcloud iam service-accounts describe ${INGRESS_SA}@${PROJECT_ID}.iam.gserviceaccount.com --format='value(uniqueId)')
 SA_EMAIL=${INGRESS_SA}@${PROJECT_ID}.iam.gserviceaccount.com
 sed -i "s/# PLACEHOLDER FOR GATEWAY_SA_ID/alpha.istio.io\/canonical-serviceaccounts: \"${SA_ID}\"/g" install/kubernetes/istio-demo-auth.yaml
 sed -i "s/# PLACEHOLDER FOR GATEWAY_SA_NAME/cloud.google.com\/service-account: \"${SA_EMAIL}\"/g" install/kubernetes/istio-demo-auth.yaml
}

inject_bookinfo() {
 NAME_PLACEHOLDERS=("REVIEW_SA" "DETAIL_SA" "RATING_SA" "PRODUCTPAGE_SA")
 ID_PLACEHOLDERS=("REVIEW_SA_ID" "DETAIL_SA_ID" "RATING_SA_ID" "PRODUCTPAGE_SA_ID")
 len=${#APP_ACCOUNTS[@]}

 for (( i=0; i<${len}; i++ ))
 do
    SA_HOLDER=${NAME_PLACEHOLDERS[$i]}
    ID_HOLDER=${ID_PLACEHOLDERS[$i]}
    SA=${APP_ACCOUNTS[$i]}

    gcloud iam service-accounts create ${SA} --display-name ${SA}
    gcloud iam service-accounts describe ${SA}@${PROJECT_ID}.iam.gserviceaccount.com
    SA_ID=$(gcloud iam service-accounts describe ${SA}@${PROJECT_ID}.iam.gserviceaccount.com --format='value(uniqueId)')
    SA_EMAIL=${SA}@${PROJECT_ID}.iam.gserviceaccount.com

    sed -i "s/# PLACEHOLDER FOR ${ID_HOLDER}/alpha.istio.io\/canonical-serviceaccounts: \"${SA_ID}\"/g" samples/bookinfo/platform/kube/bookinfo.yaml
    sed -i "s/# PLACEHOLDER FOR ${SA_HOLDER}/cloud.google.com\/service-account: \"${SA_EMAIL}\"/g" samples/bookinfo/platform/kube/bookinfo.yaml
 done
}

main() {
 PROJECT_ID=$(gcloud config get-value project)
 INGRESS_SA="ingress-sa"
 APP_ACCOUNTS=("reviews-sa" "details-sa" "ratings-sa" "productpage-sa")

 inject_ingress
 inject_bookinfo
 exit 0
}

main "$@"
