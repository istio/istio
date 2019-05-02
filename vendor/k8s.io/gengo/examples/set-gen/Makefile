TOOL=set-gen

test:
	@if ! git diff --quiet HEAD; then \
	    echo "FAIL: git client is not clean"; \
	    false; \
	fi
	@go build -o /tmp/$(TOOL)
	@PKGS=$$(go list ./sets/types  | paste -sd' ' -); \
	/tmp/$(TOOL) --logtostderr --v=4 -i $$(echo $$PKGS | sed 's/ /,/g') -O zz_generated -o ./sets
	@if ! git diff --quiet HEAD; then \
	    echo "FAIL: output files changed"; \
	    git diff; \
	    false; \
	fi
