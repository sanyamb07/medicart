#!/usr/bin/env bash
# Source this to get the toolchains MediCart expects:  . infra/scripts/devenv.sh
#
# Why this exists: this machine's system Go is 1.19 (too old) and exports a
# GOROOT=/usr/local/go. That exported GOROOT follows whichever `go` binary is
# first on PATH, so a newer toolchain would read the OLD GOROOT's defaults and
# end up with an empty GOPROXY ("GOPROXY list is not the empty string, but
# contains no entries"). Unsetting GOROOT lets each toolchain use its own.

# Let the Go binary determine its own GOROOT.
unset GOROOT

MEDICART_GO_DIR="${MEDICART_GO_DIR:-$HOME/.local/toolchains/go1.24.8}"
if [ -x "$MEDICART_GO_DIR/bin/go" ]; then
  export PATH="$MEDICART_GO_DIR/bin:$PATH"
fi

# Node 22 via nvm (system/default nvm node here was 16, too old for Vite 6).
if [ -s "$HOME/.nvm/nvm.sh" ]; then
  # shellcheck disable=SC1091
  . "$HOME/.nvm/nvm.sh"
  nvm use 22 >/dev/null 2>&1 || true
fi

echo "go:   $(command -v go)  $(go version 2>/dev/null | awk '{print $3}')"
echo "node: $(command -v node) $(node --version 2>/dev/null)"
