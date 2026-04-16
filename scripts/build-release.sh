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
Usage: ./scripts/build-release.sh [--version VERSION] [--os linux|windows] [--arch amd64|arm64 | --all]

Builds a local release artifact under dist/.

Options:
  --version VERSION  Release version label. Default: dev
  --os OS            Target operating system. Default: current Go OS
  --arch ARCH        Target architecture. Default: current Go architecture
  --all              Build release artifacts for all supported targets
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
  local os=""
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
      --os)
        shift
        [[ $# -gt 0 ]] || { log_error "--os requires a value"; exit 1; }
        os="$1"
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
  require_command sha256sum

  if [[ "${build_all}" == "true" && ( -n "${arch}" || -n "${os}" ) ]]; then
    log_error "--os/--arch and --all cannot be used together"
    exit 1
  fi

  if [[ "${build_all}" == "true" ]]; then
    build_release "${version}" linux amd64
    build_release "${version}" linux arm64
    build_release "${version}" windows amd64
    return
  fi

  if [[ -z "${os}" ]]; then
    os="$(go env GOOS)"
  fi

  if [[ -z "${arch}" ]]; then
    arch="$(go env GOARCH)"
  fi

  build_release "${version}" "${os}" "${arch}"
}

build_release() {
  local version="$1"
  local target_os="$2"
  local arch="$3"
  local host_os=""
  local host_arch=""
  local goarch=""
  local output_name=""
  local artifact_path=""
  local binary_name=""
  local cc=""

  case "${target_os}:${arch}" in
    linux:amd64|linux:arm64|windows:amd64)
      goarch="${arch}"
      ;;
    *)
      log_error "Unsupported target: ${target_os}/${arch}"
      exit 1
      ;;
  esac

  host_os="$(go env GOOS)"
  host_arch="$(go env GOARCH)"

  case "${target_os}" in
    linux)
      binary_name="${APP_NAME}"
      ;;
    windows)
      binary_name="${APP_NAME}.exe"
      ;;
  esac

  if [[ "${target_os}" == "${host_os}" && "${goarch}" == "${host_arch}" ]]; then
    cc="${CC:-cc}"
  else
    case "${target_os}:${goarch}" in
      linux:amd64)
        cc="${CC_AMD64:-x86_64-linux-gnu-gcc}"
        ;;
      linux:arm64)
        cc="${CC_ARM64:-aarch64-linux-gnu-gcc}"
        ;;
      windows:amd64)
        cc="${CC_WINDOWS_AMD64:-x86_64-w64-mingw32-gcc}"
        ;;
    esac
  fi

  if ! command -v "${cc}" >/dev/null 2>&1; then
    log_error "Cross-Compiler not found for ${target_os}/${goarch}: ${cc}"
    log_error "Set the appropriate compiler in CC, CC_AMD64, CC_ARM64, or CC_WINDOWS_AMD64 before building."
    exit 1
  fi

  mkdir -p "${DIST_DIR}"
  output_name="${APP_NAME}-${target_os}-${goarch}"
  if [[ "${target_os}" == "windows" ]]; then
    output_name="${output_name}.exe"
  fi
  artifact_path="${DIST_DIR}/${output_name}"

  log_section "Build Local Release"
  log_step "Version label: ${version}"
  log_step "Target OS: ${target_os}"
  log_step "Target architecture: ${goarch}"
  log_step "C compiler: ${cc}"

  cd "${SOURCE_DIR}"

  log_step "Building GUI binary"
  CGO_ENABLED=1 CC="${cc}" GOOS="${target_os}" GOARCH="${goarch}" go build \
    -ldflags "-X main.version=${version}" \
    -o "${artifact_path}" \
    ./cmd/gui
  log_success "Binary built: ${artifact_path}"

  log_step "Writing checksum"
  sha256sum "${artifact_path}" > "${artifact_path}.sha256"
  log_success "Checksum written: ${artifact_path}.sha256"

  printf '%sArtifact:%s %s\n' "${C_BOLD}" "${C_RESET}" "${artifact_path}"
}

main "$@"
