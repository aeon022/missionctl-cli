#!/bin/bash
set -e
cd "$(dirname "$0")"
go build -o missionctl .
sudo mv missionctl /usr/local/bin/missionctl
echo "missionctl installed"
