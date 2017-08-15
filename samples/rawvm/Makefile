
FORTIO_IMAGE:='gcr.io/istio-testing/fortio:0.2.1-pre1'
HUB:=gcr.io/istio-testing
TAG:=ldemailly7
VERBOSITY:=2
NAMESPACE:=default

YAMLS:=k8services-istio.yaml k8services.yaml k8cli.yaml
# Was needed when images were missing (#553)
#PATCH_HUB:='| sed -e "s!docker.io/istio!gcr.io/istio-io!g"'

deploy: k8services-istio client

client: k8cli.yaml
	kubectl apply -f $<

k8services-istio: k8services-istio.yaml
	kubectl apply -f $<

k8services-istio.yaml: k8services.yaml
	istioctl kube-inject -n $(NAMESPACE) --hub $(HUB) --tag $(TAG) --verbosity $(VERBOSITY) -f $< $(PATCH_HUB) > $@

k8services: k8services.yaml
	kubectl apply -f $<

%.yaml: %.yaml.in
	sed -e "s!FORTIO_IMAGE!$(FORTIO_IMAGE)!g" < $< > $@

dump-images:
	kubectl get pods --all-namespaces -o jsonpath="{..image}" | \
		tr -s '[[:space:]]' '\n' | sort | uniq -c

clean:
	-kubectl delete -f k8services-istio.yaml
	-kubectl delete -f k8services.yaml
	$(RM) $(YAMLS)

.PHONY: clean k8services k8services-istio dump-images client deploy
