
FORTIO_IMAGE:='istio/fortio:latest'
VERBOSITY:=2
NAMESPACE:=fortio

YAMLS:=k8services-istio.yaml k8services.yaml k8cli.yaml
# Was needed when images were missing (#553)
#PATCH_HUB:='| sed -e "s!docker.io/istio!gcr.io/istio-io!g"'

deploy: k8services-istio client

client: k8cli.yaml
	kubectl apply -n $(NAMESPACE) -f $<

k8services-istio: k8services-istio.yaml
	kubectl apply -n $(NAMESPACE) -f $<

k8services-istio.yaml: k8services.yaml
	istioctl kube-inject -n $(NAMESPACE) --hub $(PILOT_HUB) --tag $(PILOT_TAG) --verbosity $(VERBOSITY) -f $< $(PATCH_HUB) > $@

k8services: k8services.yaml
	kubectl apply -n $(NAMESPACE) -f $<

%.yaml: %.yaml.in
	sed -e "s!FORTIO_IMAGE!$(FORTIO_IMAGE)!g" < $< > $@

dump-images:
	kubectl get pods --all-namespaces -o jsonpath="{..image}" | \
		tr -s '[[:space:]]' '\n' | sort | uniq -c

clean:
	-kubectl delete -n $(NAMESPACE) -f k8services-istio.yaml
	-kubectl delete -n $(NAMESPACE) -f k8services.yaml
	$(RM) $(YAMLS)

.PHONY: clean k8services k8services-istio dump-images client deploy
