#!/usr/bin/env bash
# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail

version="${1:?usage: envtest-assets.sh <kubernetes-version>}"
goos="$(go env GOOS)"
goarch="$(go env GOARCH)"

case "${version}:${goos}/${goarch}" in
1.35.6:linux/amd64)
	# Kubernetes v1.35.6 embeds the etcd v3.6.5 client/server family.
	etcd_version="3.6.5"
	;;
1.36.2:linux/amd64)
	# Kubernetes v1.36.2 embeds the etcd v3.6.8 client/server family.
	etcd_version="3.6.8"
	;;
*)
	echo "no direct envtest asset mapping for Kubernetes ${version} on ${goos}/${goarch}" >&2
	exit 1
	;;
esac

cache_root="${XDG_CACHE_HOME:-${HOME:-/tmp}/.cache}/kubeseer/envtest"
assets_dir="${cache_root}/${version}/${goos}-${goarch}"

if [[ -x "${assets_dir}/etcd" && -x "${assets_dir}/kube-apiserver" && -x "${assets_dir}/kubectl" ]]; then
	printf '%s\n' "${assets_dir}"
	exit 0
fi

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/kubeseer-envtest.XXXXXXXX")"
trap 'rm -rf "${work_dir}"' EXIT

curl --fail --location --retry 3 --silent --show-error \
	--output "${work_dir}/kube-apiserver" \
	"https://dl.k8s.io/release/v${version}/bin/${goos}/${goarch}/kube-apiserver"
curl --fail --location --retry 3 --silent --show-error \
	--output "${work_dir}/kubectl" \
	"https://dl.k8s.io/release/v${version}/bin/${goos}/${goarch}/kubectl"
curl --fail --location --retry 3 --silent --show-error \
	--output "${work_dir}/etcd.tar.gz" \
	"https://github.com/etcd-io/etcd/releases/download/v${etcd_version}/etcd-v${etcd_version}-${goos}-${goarch}.tar.gz"

mkdir -p "${work_dir}/etcd"
tar --extract --gzip --file "${work_dir}/etcd.tar.gz" \
	--directory "${work_dir}/etcd" --strip-components=1
mkdir -p "${work_dir}/assets"
install -m 0755 "${work_dir}/kube-apiserver" "${work_dir}/assets/kube-apiserver"
install -m 0755 "${work_dir}/kubectl" "${work_dir}/assets/kubectl"
install -m 0755 "${work_dir}/etcd/etcd" "${work_dir}/assets/etcd"

mkdir -p "${cache_root}/${version}"
mv "${work_dir}/assets" "${assets_dir}"
printf '%s\n' "${assets_dir}"
