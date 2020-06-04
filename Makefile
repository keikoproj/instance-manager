export GO111MODULE=on

GO_MIN_VERSION := 11100 # go1.11
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

# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true"

.PHONY: all
all: check-go test clean manager

# Run tests
.PHONY: test
test: generate fmt vet manifests
	go test -v ./controllers/... ./api/... -coverprofile coverage.txt

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
	kubectl auth reconcile -f config/rbac/role_binding.yaml
	kubectl apply -f config/crd/bases

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
.PHONY: deploy
deploy: manifests
	kubectl apply -f config/crd/bases
	kustomize build config/default | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
.PHONY: manifests
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=instance-manager webhook paths="./api/...;./controllers/..." output:crd:artifacts:config=config/crd/bases

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
	@echo "updating kustomize image patch file for manager resource"
	sed -i'' -e 's@image: .*@image: '"${IMG}"'@' ./config/default/manager_image_patch.yaml

# Push the docker image
.PHONY: docker-push
docker-push:
	docker push ${IMG}

# find or download controller-gen
# download controller-gen if necessary
.PHONY: controller-gen
controller-gen:
ifeq (, $(shell which controller-gen))
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.2.9
CONTROLLER_GEN=$(shell go env GOPATH)/bin/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

.PHONY: check-go
check-go:
ifeq ($(GO_VERSION_CHECK),0)
        $(error go1.11 or higher is required)
endif

.PHONY: lint
lint: check-go
	@echo "golint $(LINTARGS)"
	@for pkg in $(shell go list ./...) ; do \
		golint $(LINTARGS) $$pkg ; \
	done

.PHONY: vendor
vendor:
	go mod tidy
	go mod vendor

.PHONY: clean
clean:
	@rm -rf ./bin	
