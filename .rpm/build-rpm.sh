#!/bin/bash
# Builds a binary RPM from an already-built Caravel binary.
#
#   build-rpm.sh <binary-path> <version> <rpm-arch>
#
# Run inside a CentOS Stream container with the repository mounted at /rpmbuild;
# see .github/workflows/release.yml. To do it by hand:
#
#   CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o build/caravel ./cmd/caravel
#   podman run --rm -v .:/rpmbuild:Z quay.io/centos/centos:stream10 \
#     /rpmbuild/.rpm/build-rpm.sh build/caravel 1.0.0 aarch64
#
# Nothing is compiled here: the Go binary is cross-compiled by the caller, which
# is why one x86_64 container can produce an aarch64 package.
set -eux

binary="${1}"
version="${2}"
arch="${3}"

dnf install -y rpmdevtools systemd-rpm-macros
rpmdev-setuptree

# The spec takes its version from a macro rather than parsing a tag, so that
# turning v1.2.3 into 1.2.3 stays the caller's business.
echo "%appversion ${version}" >> ~/.rpmmacros

cp "/rpmbuild/${binary}" ~/rpmbuild/SOURCES/caravel
cp /rpmbuild/.rpm/caravel.conf ~/rpmbuild/SOURCES/
cp /rpmbuild/.rpm/caravel.service ~/rpmbuild/SOURCES/
cp /rpmbuild/.rpm/caravel.spec ~/rpmbuild/SPECS/
# %license and %doc resolve against the build directory, and %prep unpacks
# nothing, so these have to be put there directly.
cp /rpmbuild/LICENSE /rpmbuild/README.md ~/rpmbuild/BUILD/

rpmbuild -bb --target "${arch}" ~/rpmbuild/SPECS/caravel.spec

cp ~/rpmbuild/RPMS/"${arch}"/caravel-*.rpm /rpmbuild/
