package main

import (
	"fmt"
	"os/exec"
)

func LaunchBizHawk(bizhawkPath, romPath, luaScript string) error {
	cmd := exec.Command(bizhawkPath, romPath, "--lua="+luaScript)
	fmt.Println("Launching BizHawk:", cmd.String())
	return cmd.Start()
}
