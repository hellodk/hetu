.PHONY: build test test-e2e tidy docker-build docker-build-lean docker-push helm-deploy helm-template e2e-phase0 run stop status doctor hooks-install

# --- Configuration ---
REGISTRY   ?= hellodk
VERSION    ?= 7.0.0
SERVICES   := collector analyzer
DASHBOARD  := dashboard
ENV        ?= dev
NAMESPACE  ?= cluster-intel
VALUES     ?= values-$(ENV).yaml

# --- Go builds ---
build:
	@for d in src/collector src/analyzer; do \
		echo "Building $$d..."; \
		(cd $$d && GOWORK=off go build ./...); \
	done

test:
	@for d in pkg/config pkg/store pkg/bus pkg/kube pkg/llm pkg/types pkg/middleware \
	          src/collector src/analyzer; do \
		echo "Testing $$d..."; \
		(cd $$d && go test ./...); \
	done

test-e2e:
	@echo "Running Playwright dashboard tests..."
	@cd src/dashboard && npm run test:e2e

tidy:
	@for d in pkg/config pkg/store pkg/bus pkg/kube pkg/llm pkg/types pkg/middleware \
	          src/collector src/analyzer; do \
		echo "Tidying $$d..."; \
		(cd $$d && go mod tidy); \
	done

# --- Docker images ---
# Full build: includes LB log (AWS SDK) support
docker-build:
	@echo "Building docker image cluster-intel-collector:$(VERSION) (full — includes lblogs)..."
	docker build -t $(REGISTRY)/cluster-intel-collector:$(VERSION) \
		-t $(REGISTRY)/cluster-intel-collector:latest \
		-f src/collector/Dockerfile .
	@echo "Building docker image cluster-intel-analyzer:$(VERSION)..."
	docker build -t $(REGISTRY)/cluster-intel-analyzer:$(VERSION) \
		-t $(REGISTRY)/cluster-intel-analyzer:latest \
		-f src/analyzer/Dockerfile .
	@echo "Building docker image cluster-intel-$(DASHBOARD):$(VERSION)..."
	docker build -t $(REGISTRY)/cluster-intel-$(DASHBOARD):$(VERSION) \
		-t $(REGISTRY)/cluster-intel-$(DASHBOARD):latest \
		-f src/$(DASHBOARD)/Dockerfile src/$(DASHBOARD)/

# Lean build: excludes AWS SDK via nolblogs tag (~7 MB smaller collector image)
docker-build-lean:
	@echo "Building docker image cluster-intel-collector:$(VERSION)-lean (no lblogs)..."
	docker build -t $(REGISTRY)/cluster-intel-collector:$(VERSION)-lean \
		--build-arg BUILD_TAGS=nolblogs \
		-f src/collector/Dockerfile .
	@echo "Building docker image cluster-intel-analyzer:$(VERSION)..."
	docker build -t $(REGISTRY)/cluster-intel-analyzer:$(VERSION) \
		-t $(REGISTRY)/cluster-intel-analyzer:latest \
		-f src/analyzer/Dockerfile .
	@echo "Building docker image cluster-intel-$(DASHBOARD):$(VERSION)..."
	docker build -t $(REGISTRY)/cluster-intel-$(DASHBOARD):$(VERSION) \
		-t $(REGISTRY)/cluster-intel-$(DASHBOARD):latest \
		-f src/$(DASHBOARD)/Dockerfile src/$(DASHBOARD)/

docker-push:
	@for svc in $(SERVICES) $(DASHBOARD); do \
		docker push $(REGISTRY)/cluster-intel-$$svc:$(VERSION); \
		docker push $(REGISTRY)/cluster-intel-$$svc:latest; \
	done

# --- Helm ---
helm-deps:
	helm dependency build deploy/helm/cluster-intel/

helm-template:
	helm template cluster-intel deploy/helm/cluster-intel/ --namespace $(NAMESPACE)

helm-deploy:
	helm upgrade --install cluster-intel deploy/helm/cluster-intel/ \
		--namespace $(NAMESPACE) --create-namespace \
		$(if $(wildcard $(VALUES)),-f $(VALUES),) \
		--set collector.image.repository=$(REGISTRY)/cluster-intel-collector \
		--set collector.image.tag=$(VERSION) \
		--set analyzer.image.repository=$(REGISTRY)/cluster-intel-analyzer \
		--set analyzer.image.tag=$(VERSION) \
		--set dashboard.image.repository=$(REGISTRY)/cluster-intel-dashboard \
		--set dashboard.image.tag=$(VERSION) \
		--wait --timeout 5m

# --- Local dev orchestration (delegates to scripts/run-local.sh) ---
# Uses --yes so all env-file values are accepted silently. Run the script
# directly (without --yes) for the interactive prompt-and-validate flow.
run:
	./scripts/run-local.sh start --yes -e $(ENV)

stop:
	./scripts/run-local.sh stop

status:
	./scripts/run-local.sh status

doctor:
	./scripts/run-local.sh doctor

# --- E2E ---
e2e-phase0:
	./scripts/e2e-phase0.sh

# --- Git hooks ---
hooks-install:
	./.githooks/install
