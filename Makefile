export GO111MODULE=on

CONTROLLER_GEN_VERSION := v0.17.2
GO_MIN_VERSION := 12000 # go1.20

define generate_int_from_semver
  echo $(1) |cut -dv -f2 |awk '{split($$0,a,"."); print  a[3]+(100*a[2])+(10000* a[1])}'
endef

CONTROLLER_GEN_VERSION_CHECK = \
  $(shell expr \
    $(shell $(call generate_int_from_semver,$(shell $(CONTROLLER_GEN) --version | awk '{print $$2}' | cut -dv -f2))) \
    \>= $(shell $(call generate_int_from_semver,$(shell echo $(CONTROLLER_GEN_VERSION) | cut -dv -f2))) \
  )

GO_VERSION_CHECK := \
  $(shell expr \
    $(shell go version | \
      awk '{print $$3}' | \
      cut -do -f2 | \
      sed -e 's/\.\([0-9][0-9]\)/\1/g' -e 's/\.\([0-9]\)/0\1/g' -e 's/^[0-9]\{3,4\}$$/&00/' \
    ) \>= $(GO_MIN_VERSION) \
  )

# Default Go linker flags.
GO_LDFLAGS ?= -ldflags="-s -w"

# Image URL to use all building/pushing image targets
IMG ?= instance-manager:latest
INSTANCEMGR_TAG ?= latest

.PHONY: all
all: check-go lint test clean manager

# Run tests
.PHONY: test
test: generate fmt vet manifests
	go test ./controllers/... ./api/... -coverprofile coverage.txt

.PHONY: bdd
bdd:
	go test -timeout 60m -v ./test-bdd/ --godog.stop-on-failure

.PHONY: wip
wip:
	go test -timeout 60m -v ./test-bdd/ --godog.tags "@wip"

.PHONY: coverage
coverage:
	go test -coverprofile coverage.txt -v ./controllers/...
	go tool cover -html=coverage.txt -o coverage.html

# Build manager binary
.PHONY: manager
manager: generate fmt vet
	go build -o bin/manager main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
.PHONY: run
run: generate fmt vet
	go run ./main.go

# Install CRDs into a cluster
.PHONY: install
install: manifests
	kubectl apply -f config/rbac/service_account.yaml
	kubectl auth reconcile -f config/rbac/role.yaml
	kubectl auth reconcile -f config/rbac/strategy_role.yaml
	kubectl auth reconcile -f config/rbac/role_binding.yaml
	kubectl auth reconcile -f config/rbac/strategy_role_binding.yaml
	kubectl apply -f config/crd/bases

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
.PHONY: deploy
deploy: manifests
	kubectl apply -f config/crd/bases
	kustomize build config/default | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
.PHONY: manifests
manifests: controller-gen
	$(CONTROLLER_GEN) rbac:roleName=instance-manager crd webhook paths="./api/...;./controllers/..." output:crd:artifacts:config=config/crd/bases

# Run go fmt against code
.PHONY: fmt
fmt:
	go fmt ./...

# Run go vet against code
.PHONY: vet
vet:
	go vet ./...

# Generate code
.PHONY: generate
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile=./hack/boilerplate.go.txt paths=./api/...

# Build the docker image
.PHONY: docker-build
docker-build:
	docker build . -t ${IMG}

# Push the docker image
.PHONY: docker-push
docker-push:
	docker push ${IMG}

LOCALBIN = $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

# Update controller-gen installation to better support ARM architectures
CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)

GOLANGCI_LINT_VERSION := v2.1.1
GOLANGCI_LINT = $(shell pwd)/bin/golangci-lint
.PHONY: golangci-lint
$(GOLANGCI_LINT): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: check-go
check-go:
ifeq ($(GO_VERSION_CHECK),0)
        $(error go 1.20 or higher is required)
endif

.PHONY: lint
lint: check-go $(GOLANGCI_LINT)
	@echo "Running golangci-lint"
	$(GOLANGCI_LINT) run ./...

.PHONY: clean
clean:
	@rm -rf ./bin
