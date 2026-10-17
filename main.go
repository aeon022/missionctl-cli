package main

import (
	"github.com/aeon022/missionctl-cli/cmd"
	"github.com/aeon022/missionctl-core/secure"
)

func main() {
	secure.Init("missionctl") // owner-only files from the first byte
	cmd.Execute()
}
