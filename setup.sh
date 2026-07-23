#!/bin/bash
set -e
cd "$(dirname "$0")"
go build -o missionctl .
mkdir -p ~/.local/bin
mv missionctl ~/.local/bin/missionctl
echo "✓ missionctl installed to ~/.local/bin/missionctl"
