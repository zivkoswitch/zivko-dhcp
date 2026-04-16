#!/usr/bin/env bash

set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "This script must be run as root." >&2
  exit 1
fi

export DEBIAN_FRONTEND=noninteractive

host_arch="$(dpkg --print-architecture)"
foreign_arch=""
case "${host_arch}" in
  amd64)
    foreign_arch="arm64"
    ;;
  arm64)
    foreign_arch="amd64"
    ;;
  *)
    echo "Unsupported Debian host architecture: ${host_arch}" >&2
    exit 1
    ;;
esac

if ! dpkg --print-foreign-architectures | grep -qx "${foreign_arch}"; then
  dpkg --add-architecture "${foreign_arch}"
fi

configure_ubuntu_multiarch_sources() {
  local ubuntu_sources="/etc/apt/sources.list.d/ubuntu.sources"
  local foreign_sources="/etc/apt/sources.list.d/zivko-cross-build.sources"
  local suites=""
  local components=""
  local signed_by=""
  local foreign_uri=""
  local temp_file=""

  if [[ ! -f "${ubuntu_sources}" ]]; then
    return
  fi

  suites="$(sed -n 's/^Suites: //p' "${ubuntu_sources}" | head -n1)"
  components="$(sed -n 's/^Components: //p' "${ubuntu_sources}" | head -n1)"
  signed_by="$(sed -n 's/^Signed-By: //p' "${ubuntu_sources}" | head -n1)"

  if [[ -z "${suites}" || -z "${components}" || -z "${signed_by}" ]]; then
    echo "Could not parse ${ubuntu_sources}" >&2
    exit 1
  fi

  if ! grep -q '^Architectures:' "${ubuntu_sources}"; then
    temp_file="$(mktemp)"
    awk -v host_arch="${host_arch}" '
      /^Signed-By:/ && !added {
        print "Architectures: " host_arch
        added=1
      }
      { print }
      END {
        if (!added) {
          print "Architectures: " host_arch
        }
      }
    ' "${ubuntu_sources}" > "${temp_file}"
    mv "${temp_file}" "${ubuntu_sources}"
  fi

  case "${foreign_arch}" in
    amd64)
      foreign_uri="http://archive.ubuntu.com/ubuntu/"
      ;;
    arm64)
      foreign_uri="http://ports.ubuntu.com/ubuntu-ports/"
      ;;
    *)
      echo "Unsupported foreign architecture: ${foreign_arch}" >&2
      exit 1
      ;;
  esac

  cat > "${foreign_sources}" <<EOF
Types: deb
URIs: ${foreign_uri}
Suites: ${suites}
Components: ${components}
Architectures: ${foreign_arch}
Signed-By: ${signed_by}
EOF
}

configure_ubuntu_multiarch_sources
apt_ubuntu_only() {
  local source_dir=""
  source_dir="$(mktemp -d)"
  trap 'rm -rf "${source_dir}"' RETURN

  cp /etc/apt/sources.list.d/ubuntu.sources "${source_dir}/ubuntu.sources"
  if [[ -f /etc/apt/sources.list.d/zivko-cross-build.sources ]]; then
    cp /etc/apt/sources.list.d/zivko-cross-build.sources "${source_dir}/zivko-cross-build.sources"
  fi

  apt-get \
    -o Dir::Etc::sourcelist=/dev/null \
    -o Dir::Etc::sourceparts="${source_dir}" \
    -o Acquire::Languages=none \
    "$@"
}

apt_ubuntu_only update
apt_ubuntu_only install -y \
  golang-go \
  gcc \
  gcc-x86-64-linux-gnu \
  gcc-aarch64-linux-gnu \
  mingw-w64 \
  pkg-config \
  zip \
  tar \
  libgl1-mesa-dev \
  xorg-dev \
  "libgl1-mesa-dev:${foreign_arch}" \
  "libx11-dev:${foreign_arch}" \
  "libxrandr-dev:${foreign_arch}" \
  "libxxf86vm-dev:${foreign_arch}" \
  "libxi-dev:${foreign_arch}" \
  "libxcursor-dev:${foreign_arch}" \
  "libxinerama-dev:${foreign_arch}"

echo "Development tool installation for Linux amd64/arm64 and Windows amd64 builds completed."
echo "Installed native ${host_arch} toolchain plus foreign-architecture GUI libs for ${foreign_arch}."
