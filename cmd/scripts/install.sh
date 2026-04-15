#!/usr/bin/env bash

set -euo pipefail

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly REPO_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
readonly DEFAULT_BIN_DIR="/usr/local/bin"
readonly APP_NAME="zivko-dhcp"
readonly SERVICE_NAME="zivko-dhcp-daemon.service"
readonly SYSTEMD_DIR="/etc/systemd/system"

if [[ -t 1 ]]; then
  readonly C_RESET=$'\033[0m'
  readonly C_BOLD=$'\033[1m'
  readonly C_BLUE=$'\033[34m'
  readonly C_GREEN=$'\033[32m'
  readonly C_YELLOW=$'\033[33m'
  readonly C_RED=$'\033[31m'
else
  readonly C_RESET=""
  readonly C_BOLD=""
  readonly C_BLUE=""
  readonly C_GREEN=""
  readonly C_YELLOW=""
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

log_warn() {
  printf '%s[WARN]%s %s\n' "${C_YELLOW}" "${C_RESET}" "$1"
}

log_error() {
  printf '%s[ERROR]%s %s\n' "${C_RED}" "${C_RESET}" "$1" >&2
}

usage() {
  cat <<'EOF'
Usage: ./scripts/install.sh [--artifact PATH] [--bin-dir PATH] [--skip-packages]

Installs previously built local release binaries or a release tarball.
If no --artifact is given and the extracted release files are present next to this script,
the installer uses those local files directly.

Options:
  --artifact PATH    Path to a local binary or .tar.gz release artifact.
                     If omitted, the installer auto-detects an artifact in dist/.
  --bin-dir PATH     Target directory for the executable. Default: /usr/local/bin
  --skip-packages    Skip installing runtime packages.
  -h, --help         Show this help text.
EOF
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    log_error "Required command not found: $1"
    exit 1
  fi
}

run_cmd() {
  local description="$1"
  shift

  log_step "${description}"
  if "$@"; then
    log_success "${description}"
  else
    log_error "Failed: ${description}"
    exit 1
  fi
}

detect_artifact() {
  if [[ -f "${SCRIPT_DIR}/${APP_NAME}" && -f "${SCRIPT_DIR}/${SERVICE_NAME}" ]]; then
    printf '%s\n' "${SCRIPT_DIR}"
    return
  fi

  local candidates=()

  if [[ -d "${REPO_DIR}/dist" ]]; then
    while IFS= read -r line; do
      candidates+=("$line")
    done < <(find "${REPO_DIR}/dist" -maxdepth 1 -type f \( -name "${APP_NAME}-linux-*.tar.gz" -o -name "${APP_NAME}-linux-*" \) | sort)
  fi

  if [[ "${#candidates[@]}" -eq 0 ]]; then
    log_error "No release artifact found in ${REPO_DIR}/dist"
    log_error "Build one first with ./scripts/build-release.sh"
    exit 1
  fi

  printf '%s\n' "${candidates[-1]}"
}

install_runtime_packages() {
  require_command sudo
  require_command apt-get

  run_cmd "Installing runtime packages" \
    sudo apt-get update
  run_cmd "Ensuring GUI runtime dependencies are present" \
    sudo apt-get install -y \
      libgl1 \
      libx11-6 \
      libxcursor1 \
      libxrandr2 \
      libxinerama1 \
      libxi6 \
      libxxf86vm1
}

install_binaries() {
  local source_path="$1"
  local bin_dir="$2"
  local tmp_dir=""
  local parent_dir=""
  local use_sudo=0
  local -a binary_names=("${APP_NAME}" "${SERVICE_NAME}")

  require_command install

  if [[ -d "${source_path}" ]]; then
    tmp_dir="${source_path%/}"
  elif [[ "${source_path}" == *.tar.gz ]]; then
    require_command tar
    tmp_dir="$(mktemp -d)"
    trap '[[ -n "${tmp_dir:-}" && "${tmp_dir}" == /tmp/* ]] && rm -rf "${tmp_dir}"' EXIT
    run_cmd "Extracting release archive" tar -xzf "${source_path}" -C "${tmp_dir}"
  else
    log_error "Direct single-binary install is no longer supported. Use the release tar.gz artifact."
    exit 1
  fi

  for binary_name in "${binary_names[@]}"; do
    if [[ -f "${tmp_dir}/${binary_name}" && ! -x "${tmp_dir}/${binary_name}" ]]; then
      chmod +x "${tmp_dir}/${binary_name}"
    fi
  done

  if [[ -d "${bin_dir}" ]]; then
    if [[ ! -w "${bin_dir}" ]]; then
      use_sudo=1
    fi
  else
    parent_dir="$(dirname "${bin_dir}")"
    if [[ ! -w "${parent_dir}" ]]; then
      use_sudo=1
    fi
  fi

  if [[ "${use_sudo}" -eq 1 ]]; then
    require_command sudo
    run_cmd "Creating target directory ${bin_dir}" sudo install -d "${bin_dir}"
    for binary_name in "${binary_names[@]}"; do
      if [[ ! -f "${tmp_dir}/${binary_name}" ]]; then
        log_error "Archive does not contain ${binary_name}"
        exit 1
      fi
      local mode="0755"
      local target_dir="${bin_dir}"
      if [[ "${binary_name}" == "${SERVICE_NAME}" ]]; then
        mode="0644"
        target_dir="${SYSTEMD_DIR}"
        run_cmd "Creating target directory ${target_dir}" sudo install -d "${target_dir}"
      fi
      run_cmd "Installing ${binary_name} to ${target_dir}" sudo install -m "${mode}" "${tmp_dir}/${binary_name}" "${target_dir}/${binary_name}"
    done
  else
    run_cmd "Creating target directory ${bin_dir}" install -d "${bin_dir}"
    for binary_name in "${binary_names[@]}"; do
      if [[ ! -f "${tmp_dir}/${binary_name}" ]]; then
        log_error "Archive does not contain ${binary_name}"
        exit 1
      fi
      local mode="0755"
      local target_dir="${bin_dir}"
      if [[ "${binary_name}" == "${SERVICE_NAME}" ]]; then
        mode="0644"
        target_dir="${SYSTEMD_DIR}"
        run_cmd "Creating target directory ${target_dir}" sudo install -d "${target_dir}"
        run_cmd "Installing ${binary_name} to ${target_dir}" sudo install -m "${mode}" "${tmp_dir}/${binary_name}" "${target_dir}/${binary_name}"
        continue
      fi
      run_cmd "Installing ${binary_name} to ${target_dir}" install -m "${mode}" "${tmp_dir}/${binary_name}" "${target_dir}/${binary_name}"
    done
  fi
}

setup_systemd_service() {
  if ! command -v systemctl >/dev/null 2>&1; then
    log_warn "systemctl not found, skipping daemon service enablement"
    return
  fi
  require_command sudo
  run_cmd "Creating daemon state directory" sudo install -d /var/lib/zivko-dhcp
  run_cmd "Reloading systemd" sudo systemctl daemon-reload
  run_cmd "Enabling daemon service" sudo systemctl enable zivko-dhcp-daemon.service
  run_cmd "Restarting daemon service" sudo systemctl restart zivko-dhcp-daemon.service
}

main() {
  local artifact=""
  local bin_dir="${DEFAULT_BIN_DIR}"
  local skip_packages=0

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --artifact)
        shift
        [[ $# -gt 0 ]] || { log_error "--artifact requires a value"; exit 1; }
        artifact="$1"
        ;;
      --bin-dir)
        shift
        [[ $# -gt 0 ]] || { log_error "--bin-dir requires a value"; exit 1; }
        bin_dir="$1"
        ;;
      --skip-packages)
        skip_packages=1
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

  log_section "DHCP GUI Binary Installer"

  if [[ -z "${artifact}" ]]; then
    artifact="$(detect_artifact)"
    log_step "Using detected artifact: ${artifact}"
  else
    log_step "Using artifact: ${artifact}"
  fi

  if [[ "${skip_packages}" -eq 0 ]]; then
    install_runtime_packages
  else
    log_warn "Skipping runtime package installation"
  fi

  install_binaries "${artifact}" "${bin_dir}"
  setup_systemd_service

  log_section "Installation Complete"
  log_success "Binary installed and systemd service prepared"
  printf '%sNext:%s start the GUI with %s%s%s\n' \
    "${C_BOLD}" "${C_RESET}" "${C_BOLD}" "${bin_dir}/${APP_NAME}" "${C_RESET}"
}

main "$@"
