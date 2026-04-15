#!/usr/bin/env bash

set -euo pipefail

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly REPO_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
if [[ -f "${REPO_DIR}/cmd/go.mod" && -f "${REPO_DIR}/cmd/cmd/gui/main.go" ]]; then
  readonly SOURCE_DIR="${REPO_DIR}/cmd"
else
  readonly SOURCE_DIR="${REPO_DIR}"
fi
readonly DIST_DIR="${REPO_DIR}/dist"
readonly APP_NAME="zivko-dhcp"
readonly SERVICE_NAME="zivko-dhcp-daemon.service"
readonly INSTALLER_NAME="install.sh"

if [[ -t 1 ]]; then
  readonly C_RESET=$'\033[0m'
  readonly C_BOLD=$'\033[1m'
  readonly C_BLUE=$'\033[34m'
  readonly C_GREEN=$'\033[32m'
  readonly C_RED=$'\033[31m'
else
  readonly C_RESET=""
  readonly C_BOLD=""
  readonly C_BLUE=""
  readonly C_GREEN=""
  readonly C_RED=""
fi

log_section() {
  printf '\n%s==> %s%s\n' "${C_BLUE}${C_BOLD}" "$1" "${C_RESET}"
}

log_step() {
  printf '%s[INFO]%s %s\n' "${C_BLUE}" "${C_RESET}" "$1"
}

log_success() {
  printf '%s[OK]%s %s\n' "${C_GREEN}" "${C_RESET}" "$1"
}

log_error() {
  printf '%s[ERROR]%s %s\n' "${C_RED}" "${C_RESET}" "$1" >&2
}

usage() {
  cat <<'EOF'
Usage: ./scripts/build-release.sh [--version VERSION] [--arch amd64|arm64 | --all]

Builds a local Linux release artifact under dist/.

Options:
  --version VERSION  Release version label. Default: dev
  --arch ARCH        Target architecture. Default: current Go architecture
  --all              Build release artifacts for all supported architectures
  -h, --help         Show this help text.
EOF
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    log_error "Required command not found: $1"
    exit 1
  fi
}

main() {
  local version="dev"
  local arch=""
  local build_all="false"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --version)
        shift
        [[ $# -gt 0 ]] || { log_error "--version requires a value"; exit 1; }
        version="$1"
        ;;
      --arch)
        shift
        [[ $# -gt 0 ]] || { log_error "--arch requires a value"; exit 1; }
        arch="$1"
        ;;
      --all)
        build_all="true"
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        log_error "Unknown option: $1"
        usage
        exit 1
        ;;
    esac
    shift
  done

  require_command go
  require_command tar
  require_command sha256sum

  if [[ "${build_all}" == "true" && -n "${arch}" ]]; then
    log_error "--arch and --all cannot be used together"
    exit 1
  fi

  if [[ "${build_all}" == "true" ]]; then
    build_release "${version}" amd64
    build_release "${version}" arm64
    return
  fi

  if [[ -z "${arch}" ]]; then
    arch="$(go env GOARCH)"
  fi

  build_release "${version}" "${arch}"
}

build_release() {
  local version="$1"
  local arch="$2"
  local host_arch=""
  local goarch=""
  local staging_dir=""
  local output_name=""
  local archive_path=""
  local cc=""

  case "${arch}" in
    amd64|arm64)
      goarch="${arch}"
      ;;
    *)
      log_error "Unsupported architecture: ${arch}"
      exit 1
      ;;
  esac

  host_arch="$(go env GOARCH)"
  if [[ "${goarch}" == "${host_arch}" ]]; then
    cc="${CC:-cc}"
  else
    case "${goarch}" in
      amd64)
        cc="${CC_AMD64:-x86_64-linux-gnu-gcc}"
        ;;
      arm64)
        cc="${CC_ARM64:-aarch64-linux-gnu-gcc}"
        ;;
    esac
  fi

  if ! command -v "${cc}" >/dev/null 2>&1; then
    log_error "Cross-Compiler not found for ${goarch}: ${cc}"
    log_error "Set the appropriate compiler in CC, CC_AMD64, or CC_ARM64 before using --all."
    exit 1
  fi

  mkdir -p "${DIST_DIR}"
  staging_dir="$(mktemp -d)"
  trap '[[ -n "${staging_dir:-}" ]] && rm -rf "${staging_dir}"' RETURN

  log_section "Build Local Release"
  log_step "Version label: ${version}"
  log_step "Target architecture: ${goarch}"
  log_step "C compiler: ${cc}"

  cd "${SOURCE_DIR}"

  log_step "Building GUI binary"
  CGO_ENABLED=1 CC="${cc}" GOOS=linux GOARCH="${goarch}" go build \
    -ldflags "-X main.version=${version}" \
    -o "${staging_dir}/${APP_NAME}" \
    ./cmd/gui
  log_success "GUI binary built"

  cp "${SOURCE_DIR}/README.md" "${staging_dir}/README.md"
  cp "${SOURCE_DIR}/packaging/systemd/${SERVICE_NAME}" "${staging_dir}/${SERVICE_NAME}"
  cp "${SOURCE_DIR}/scripts/install.sh" "${staging_dir}/${INSTALLER_NAME}"
  chmod +x "${staging_dir}/${INSTALLER_NAME}"

  log_step "Packaging release archive"
  tar -czf "${archive_path}" -C "${staging_dir}" "${APP_NAME}" "${SERVICE_NAME}" "${INSTALLER_NAME}" README.md
  log_success "Archive created: ${archive_path}"

  log_step "Writing checksum"
  sha256sum "${archive_path}" > "${archive_path}.sha256"
  log_success "Checksum written: ${archive_path}.sha256"

  printf '%sArtifact:%s %s\n' "${C_BOLD}" "${C_RESET}" "${archive_path}"
  rm -rf "${staging_dir}"
  trap - RETURN
}

main "$@"
