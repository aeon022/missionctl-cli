#!/bin/bash
set -e
cd "$(dirname "$0")"
go build -o missionctl .
mkdir -p ~/.local/bin
mv missionctl ~/.local/bin/missionctl
echo "✓ missionctl installed to ~/.local/bin/missionctl"

# man pages (best effort): man missionctl
mkdir -p "$HOME/.local/share/man/man1" && "${INSTALL_DIR:-$HOME/.local/bin}/missionctl" man "$HOME/.local/share/man/man1" >/dev/null 2>&1 || true
