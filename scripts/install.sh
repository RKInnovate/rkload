#!/usr/bin/env sh
# install.sh — download and install the rkload binary on Linux or macOS.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/RKInnovate/rkload/main/scripts/install.sh | sh
#   ./scripts/install.sh                          # from a clone
#   ./scripts/install.sh --version v1.0.0         # pin a specific tag
#   ./scripts/install.sh --dir ~/.local/bin       # install elsewhere
#
# The script picks an install directory in this order:
#   1. --dir DIR if passed
#   2. RKLOAD_INSTALL_DIR if set
#   3. /usr/local/bin if writable (or sudo is available and the user
#      consents); otherwise ~/.local/bin
#
# Exits non-zero on any failure. Designed to be pasted into untrusted
# shells, so it sets -eu and explicitly checks every external command
# it depends on (curl/wget, tar, uname).

set -eu

REPO="RKInnovate/rkload"
VERSION=""
INSTALL_DIR="${RKLOAD_INSTALL_DIR:-}"

usage() {
	cat <<EOF
Usage: install.sh [--version VERSION] [--dir DIR] [--help]

  --version  Tag to install (default: latest release on GitHub)
  --dir      Directory to install rkload into. Default: /usr/local/bin
             if writable, else ~/.local/bin. Honours RKLOAD_INSTALL_DIR
             as well.
  --help     Show this help.
EOF
}

while [ $# -gt 0 ]; do
	case "$1" in
	--version)
		[ $# -ge 2 ] || { echo "install.sh: --version needs an argument" >&2; exit 2; }
		VERSION="$2"
		shift 2
		;;
	--dir)
		[ $# -ge 2 ] || { echo "install.sh: --dir needs an argument" >&2; exit 2; }
		INSTALL_DIR="$2"
		shift 2
		;;
	--help|-h)
		usage
		exit 0
		;;
	*)
		echo "install.sh: unknown argument: $1" >&2
		usage >&2
		exit 2
		;;
	esac
done

need() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "install.sh: required command '$1' not found in PATH" >&2
		exit 1
	}
}

need uname
need tar
need mktemp

# Use whichever HTTP client is present; both are common.
if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL "$1" -o "$2"; }
	fetch_stdout() { curl -fsSL "$1"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -qO "$2" "$1"; }
	fetch_stdout() { wget -qO- "$1"; }
else
	echo "install.sh: need curl or wget on PATH" >&2
	exit 1
fi

# Map uname → goreleaser archive name.
os_raw="$(uname -s)"
case "$os_raw" in
Linux)   os="linux"  ;;
Darwin)  os="darwin" ;;
*)
	echo "install.sh: unsupported OS '$os_raw' (use the Windows install.ps1 or build from source)" >&2
	exit 1
	;;
esac

arch_raw="$(uname -m)"
case "$arch_raw" in
x86_64|amd64)         arch="x86_64" ;;
aarch64|arm64)        arch="arm64"  ;;
*)
	echo "install.sh: unsupported architecture '$arch_raw'" >&2
	exit 1
	;;
esac

# Resolve "latest" if no --version was passed. The redirect target of
# /releases/latest contains the actual tag; we don't depend on the
# JSON API, which is rate-limited for unauthenticated callers.
if [ -z "$VERSION" ]; then
	latest_url=$(fetch_stdout "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null \
		| sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' \
		| head -n 1) || true
	if [ -z "$latest_url" ]; then
		# Fallback: parse the redirected Location header.
		latest_url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
			"https://github.com/${REPO}/releases/latest" 2>/dev/null \
			| sed 's|.*/tag/||') || true
	fi
	VERSION="$latest_url"
fi

if [ -z "$VERSION" ]; then
	echo "install.sh: could not determine latest version. Pass --version explicitly." >&2
	exit 1
fi

# Strip leading 'v' for the archive name (goreleaser archives use the
# bare version) but keep it for the release URL path.
version_bare="${VERSION#v}"
archive="rkload_${version_bare}_${os}_${arch}.tar.gz"
url="https://github.com/${REPO}/releases/download/${VERSION}/${archive}"

# Pick the install directory. /usr/local/bin is the conventional spot
# but requires sudo on most systems; ~/.local/bin works without root
# and only needs to be on PATH.
choose_install_dir() {
	if [ -n "$INSTALL_DIR" ]; then
		printf '%s' "$INSTALL_DIR"
		return
	fi
	if [ -w /usr/local/bin ] 2>/dev/null; then
		printf '/usr/local/bin'
		return
	fi
	# /usr/local/bin exists on most systems but isn't writable as a
	# non-root user. Try sudo (silently — no prompt yet); fall back to
	# ~/.local/bin if sudo isn't around.
	if command -v sudo >/dev/null 2>&1 && [ -d /usr/local/bin ]; then
		printf '/usr/local/bin'
		return
	fi
	printf '%s' "${HOME}/.local/bin"
}
INSTALL_DIR="$(choose_install_dir)"

# Workspace for the download/extract. trap ensures cleanup even on
# Ctrl-C — important when fetching over slow connections.
tmpdir="$(mktemp -d 2>/dev/null || mktemp -d -t rkload-install)"
trap 'rm -rf "$tmpdir"' EXIT INT TERM

echo "Installing rkload ${VERSION} (${os}/${arch}) to ${INSTALL_DIR}"
echo "  archive: ${url}"

archive_path="${tmpdir}/${archive}"
fetch "$url" "$archive_path"
tar -xzf "$archive_path" -C "$tmpdir"

if [ ! -f "${tmpdir}/rkload" ]; then
	echo "install.sh: archive did not contain a 'rkload' binary" >&2
	exit 1
fi
chmod +x "${tmpdir}/rkload"

# Ensure the install directory exists, then move (or sudo-move) the
# binary into place. We do not blindly run sudo — only when the
# directory is unwritable by the current user.
mkdir -p "$INSTALL_DIR" 2>/dev/null || true
if [ -w "$INSTALL_DIR" ]; then
	mv "${tmpdir}/rkload" "${INSTALL_DIR}/rkload"
elif command -v sudo >/dev/null 2>&1; then
	echo "  (writing to ${INSTALL_DIR} requires sudo)"
	sudo mv "${tmpdir}/rkload" "${INSTALL_DIR}/rkload"
else
	echo "install.sh: ${INSTALL_DIR} is not writable and sudo is not available." >&2
	echo "  Pass --dir DIR or set RKLOAD_INSTALL_DIR to a writable location." >&2
	exit 1
fi

echo "Installed: ${INSTALL_DIR}/rkload"
if ! printf '%s' ":$PATH:" | grep -q ":${INSTALL_DIR}:"; then
	echo
	echo "Note: ${INSTALL_DIR} is not on your PATH. Add this line to your shell rc:"
	echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
fi

"${INSTALL_DIR}/rkload" -version
