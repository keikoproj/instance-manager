#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

readonly REPO_ROOT="$(git rev-parse --show-toplevel)"

function make_fake_paths() {
  FAKE_GOPATH="$(mktemp -d)"
  trap 'rm -rf ${FAKE_GOPATH}' EXIT
  FAKE_REPOPATH="${FAKE_GOPATH}/src/github.com/keikoproj/instance-manager"
  mkdir -p "$(dirname "${FAKE_REPOPATH}")" && ln -s "${REPO_ROOT}" "${FAKE_REPOPATH}"
}

make_fake_paths

export GOPATH="${FAKE_GOPATH}"
export GO111MODULE="off"

cd "${FAKE_REPOPATH}"

CODEGEN_PKG=${CODEGEN_PKG:-$(cd "${FAKE_REPOPATH}"; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}

echo "${FAKE_REPOPATH}"
echo ${CODEGEN_PKG}

chmod +x ${CODEGEN_PKG}/*.sh

bash -x ${CODEGEN_PKG}/generate-groups.sh "client,informer,lister" \
  github.com/keikoproj/instance-manager/client github.com/keikoproj/instance-manager/api \
  "instancemgr:v1alpha1" \
  --go-header-file hack/boilerplate.go.txt
