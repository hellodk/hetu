.PHONY: build test tidy docker-build docker-push helm-deploy helm-template e2e-phase0

# --- Configuration ---
REGISTRY   ?= ghcr.io/your-org
VERSION    ?= 7.0.0
SERVICES   := collector analyzer collector-podlogs collector-lblogs
DASHBOARD  := dashboard

# --- Go builds ---
build:
	@for d in src/collector src/analyzer src/collector-podlogs src/collector-lblogs; do \
		echo "Building $$d..."; \
		(cd $$d && go build ./...); \
	done

test:
	@for d in pkg/config pkg/store pkg/bus pkg/kube pkg/llm pkg/types pkg/middleware \
	          src/collector src/analyzer src/collector-podlogs src/collector-lblogs; do \
		echo "Testing $$d..."; \
		(cd $$d && go test ./...); \
	done

tidy:
	@for d in pkg/config pkg/store pkg/bus pkg/kube pkg/llm pkg/types pkg/middleware \
	          src/collector src/analyzer src/collector-podlogs src/collector-lblogs; do \
		echo "Tidying $$d..."; \
		(cd $$d && go mod tidy); \
	done

# --- Docker images ---
docker-build:
	@for svc in $(SERVICES); do \
		echo "Building docker image cluster-intel-$$svc:$(VERSION)..."; \
		docker build -t $(REGISTRY)/cluster-intel-$$svc:$(VERSION) \
			-f src/$$svc/Dockerfile .; \
	done
	@echo "Building docker image cluster-intel-$(DASHBOARD):$(VERSION)..."
	docker build -t $(REGISTRY)/cluster-intel-$(DASHBOARD):$(VERSION) \
		-f src/$(DASHBOARD)/Dockerfile src/$(DASHBOARD)/

docker-push:
	@for svc in $(SERVICES); do \
		docker push $(REGISTRY)/cluster-intel-$$svc:$(VERSION); \
	done
	docker push $(REGISTRY)/cluster-intel-$(DASHBOARD):$(VERSION)

# --- Helm ---
helm-deps:
	helm dependency build deploy/helm/cluster-intel/

helm-template:
	helm template cluster-intel deploy/helm/cluster-intel/ --namespace cluster-intel

helm-deploy:
	helm upgrade --install cluster-intel deploy/helm/cluster-intel/ \
		--namespace cluster-intel --create-namespace \
		--set collector.image.repository=$(REGISTRY)/cluster-intel-collector \
		--set collector.image.tag=$(VERSION) \
		--set analyzer.image.repository=$(REGISTRY)/cluster-intel-analyzer \
		--set analyzer.image.tag=$(VERSION) \
		--set dashboard.image.repository=$(REGISTRY)/cluster-intel-dashboard \
		--set dashboard.image.tag=$(VERSION) \
		--wait --timeout 5m

# --- E2E ---
e2e-phase0:
	./scripts/e2e-phase0.sh
