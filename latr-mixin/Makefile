JSONNET_FMT := jsonnetfmt -n 2 --max-blank-lines 2 --string-style s --comment-style s

.PHONY: all fmt lint build dashboards_out prometheus_alerts.yaml clean jb_install

all: fmt build

fmt:
	@find . -path ./vendor -prune -o \( -name '*.libsonnet' -o -name '*.jsonnet' \) -print | \
		xargs -n 1 $(JSONNET_FMT) -i

lint: prometheus_alerts.yaml
	@find . -path ./vendor -prune -o \( -name '*.libsonnet' -o -name '*.jsonnet' \) -print | \
		while read f; do \
			$(JSONNET_FMT) "$$f" | diff -u "$$f" - || exit 1; \
		done
	@if command -v promtool >/dev/null 2>&1; then \
		promtool check rules prometheus_alerts.yaml; \
	else \
		echo "promtool not found; skipping rule check"; \
	fi

build: dashboards_out prometheus_alerts.yaml

prometheus_alerts.yaml: mixin.libsonnet config.libsonnet $(wildcard alerts/*)
	jsonnet -S alerts.jsonnet > $@

dashboards_out: mixin.libsonnet config.libsonnet $(wildcard dashboards/*)
	@mkdir -p dashboards_out
	jsonnet -J . -m dashboards_out dashboards.jsonnet

jb_install:
	jb install

clean:
	rm -rf dashboards_out prometheus_alerts.yaml
