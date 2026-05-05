#!/usr/bin/env sh
set -eu

repo="${TORQUE_REPO:-ingresslabs/torque}"
tool="${TORQUE_TOOL:-torque}"
version="${TORQUE_VERSION:-latest}"
install_dir="${TORQUE_INSTALL_DIR:-}"
os="${TORQUE_OS:-}"
arch="${TORQUE_ARCH:-}"
dry_run="${TORQUE_DRY_RUN:-0}"
checksum="${TORQUE_CHECKSUM:-1}"
token="${GITHUB_TOKEN:-${GH_TOKEN:-}}"

usage() {
  cat >&2 <<EOF
Install torque from GitHub Releases.

Usage:
  install.sh [--version <tag>] [--dir <path>] [--tool <name>] [--repo <owner/repo>]
             [--os <linux|darwin>] [--arch <amd64|arm64>] [--dry-run]
             [--skip-checksum]

Environment:
  TORQUE_VERSION       Release tag to install, or latest. Default: latest
  TORQUE_INSTALL_DIR   Install directory. Default: existing binary dir, /usr/local/bin, or ~/.local/bin
  TORQUE_REPO          GitHub repository. Default: ingresslabs/torque
  TORQUE_TOOL          Binary to install. Default: torque
  TORQUE_OS            Override detected OS
  TORQUE_ARCH          Override detected architecture
  TORQUE_DRY_RUN       Print what would happen without installing
  TORQUE_CHECKSUM      Verify sha256 when release checksums exist. Default: 1
  GH_TOKEN/GITHUB_TOKEN Token for private or rate-limited GitHub access
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version|-v)
      version="${2:-}"
      shift 2
      ;;
    --dir|-d|--install-dir)
      install_dir="${2:-}"
      shift 2
      ;;
    --repo)
      repo="${2:-}"
      shift 2
      ;;
    --tool)
      tool="${2:-}"
      shift 2
      ;;
    --os)
      os="${2:-}"
      shift 2
      ;;
    --arch)
      arch="${2:-}"
      shift 2
      ;;
    --dry-run)
      dry_run=1
      shift
      ;;
    --skip-checksum)
      checksum=0
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "unknown option: $1" >&2
      usage
      exit 2
      ;;
  esac
done

[ -n "$repo" ] || { echo "repo is required" >&2; exit 2; }
[ -n "$tool" ] || { echo "tool is required" >&2; exit 2; }
[ -n "$version" ] || { echo "version is required" >&2; exit 2; }

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 2
  }
}

need curl
need tar
need uname
need sed
need mktemp
need tr
need find
need head

normalize_os() {
  value="$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')"
  case "$value" in
    linux|darwin) printf '%s\n' "$value" ;;
    *) echo "unsupported OS: $1" >&2; exit 2 ;;
  esac
}

normalize_arch() {
  case "$1" in
    x86_64|amd64) printf '%s\n' amd64 ;;
    arm64|aarch64) printf '%s\n' arm64 ;;
    *) echo "unsupported architecture: $1" >&2; exit 2 ;;
  esac
}

if [ -z "$os" ]; then
  os="$(uname -s)"
fi
os="$(normalize_os "$os")"

if [ -z "$arch" ]; then
  arch="$(uname -m)"
fi
arch="$(normalize_arch "$arch")"

tmp="$(mktemp -d)"
cleanup() {
  rm -rf "$tmp"
}
trap cleanup EXIT INT TERM

github_curl() {
  if [ -n "$token" ]; then
    curl -fsSL \
      -H "Authorization: Bearer $token" \
      -H "Accept: application/vnd.github+json" \
      "$@"
  else
    curl -fsSL "$@"
  fi
}

if [ "$version" = "latest" ]; then
  api_url="https://api.github.com/repos/${repo}/releases/latest"
  release_json="$(github_curl "$api_url")" || {
    echo "could not read latest release from ${repo}" >&2
    exit 1
  }
  version="$(
    printf '%s' "$release_json" |
      sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
      head -n 1
  )"
  [ -n "$version" ] || {
    echo "could not determine latest release tag" >&2
    exit 1
  }
fi

asset="${tool}-${os}-${arch}-${version}.tar.gz"
base_url="https://github.com/${repo}/releases/download/${version}"
url="${base_url}/${asset}"
archive="${tmp}/${asset}"

default_install_dir() {
  if existing="$(command -v "$tool" 2>/dev/null)"; then
    case "$existing" in
      */*) printf '%s\n' "${existing%/*}"; return ;;
    esac
  fi
  if [ -d /usr/local/bin ] && { [ -w /usr/local/bin ] || command -v sudo >/dev/null 2>&1; }; then
    printf '%s\n' /usr/local/bin
    return
  fi
  printf '%s\n' "${HOME}/.local/bin"
}

if [ -z "$install_dir" ]; then
  install_dir="$(default_install_dir)"
fi

echo "repo:    ${repo}" >&2
echo "version: ${version}" >&2
echo "target:  ${os}/${arch}" >&2
echo "asset:   ${asset}" >&2
echo "dest:    ${install_dir}/${tool}" >&2

if [ "$dry_run" = "1" ]; then
  echo "dry run: would download ${url}" >&2
  exit 0
fi

echo "downloading ${asset}" >&2
github_curl -L "$url" -o "$archive" || {
  echo "could not download release asset: ${asset}" >&2
  exit 1
}

sha_cmd() {
  if command -v sha256sum >/dev/null 2>&1; then
    printf '%s\n' "sha256sum"
  elif command -v shasum >/dev/null 2>&1; then
    printf '%s\n' "shasum -a 256"
  else
    return 1
  fi
}

verify_checksum() {
  [ "$checksum" = "1" ] || return 0
  cmd="$(sha_cmd)" || {
    echo "checksum tool not found; skipping sha256 verification" >&2
    return 0
  }

  checksum_file="${tmp}/checksums.txt"
  checksum_url="${base_url}/checksums-${os}-${arch}-${version}.txt"
  if ! github_curl "$checksum_url" -o "$checksum_file" 2>/dev/null; then
    if ! github_curl "${url}.sha256" -o "$checksum_file" 2>/dev/null; then
      echo "checksums not found for ${asset}; skipping sha256 verification" >&2
      return 0
    fi
  fi

  expected="$(
    sed -n "s/^\([0-9a-fA-F][0-9a-fA-F]*\)[[:space:]][[:space:]]*.*${asset}\$/\1/p" "$checksum_file" |
      head -n 1
  )"
  if [ -z "$expected" ]; then
    echo "checksum file did not mention ${asset}; skipping sha256 verification" >&2
    return 0
  fi

  actual="$($cmd "$archive" | sed 's/[[:space:]].*//')"
  if [ "$actual" != "$expected" ]; then
    echo "sha256 mismatch for ${asset}" >&2
    echo "expected: ${expected}" >&2
    echo "actual:   ${actual}" >&2
    exit 1
  fi
  echo "verified sha256: ${actual}" >&2
}

verify_checksum

tar -xzf "$archive" -C "$tmp"
bin_path="$(find "$tmp" -type f -name "$tool" | head -n 1)"
if [ -z "$bin_path" ]; then
  echo "release archive did not contain ${tool}" >&2
  exit 1
fi

chmod 0755 "$bin_path"
if mkdir -p "$install_dir" 2>/dev/null && [ -w "$install_dir" ]; then
  if command -v install >/dev/null 2>&1; then
    install -m 0755 "$bin_path" "${install_dir}/${tool}"
  else
    cp "$bin_path" "${install_dir}/${tool}"
    chmod 0755 "${install_dir}/${tool}"
  fi
elif command -v sudo >/dev/null 2>&1; then
  sudo mkdir -p "$install_dir"
  if command -v install >/dev/null 2>&1; then
    sudo install -m 0755 "$bin_path" "${install_dir}/${tool}"
  else
    sudo cp "$bin_path" "${install_dir}/${tool}"
    sudo chmod 0755 "${install_dir}/${tool}"
  fi
else
  install_dir="${HOME}/.local/bin"
  mkdir -p "$install_dir"
  if command -v install >/dev/null 2>&1; then
    install -m 0755 "$bin_path" "${install_dir}/${tool}"
  else
    cp "$bin_path" "${install_dir}/${tool}"
    chmod 0755 "${install_dir}/${tool}"
  fi
fi

echo "installed ${tool} to ${install_dir}/${tool}" >&2
"${install_dir}/${tool}" version 2>/dev/null || true
