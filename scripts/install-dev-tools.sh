#!/usr/bin/env bash

set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "This script must be run as root." >&2
  exit 1
fi

export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y \
  golang-go \
  gcc \
  pkg-config \
  libgl1-mesa-dev \
  xorg-dev

echo "Development tool installation for local builds completed."
