package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
)

// bizhawk.go
func LaunchBizHawk(cfg *Config) (*exec.Cmd, error) {
	exe := cfg.BizHawkPath

	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("BizHawk is only supported on Windows in this setup")
	}

	args := []string{}
	if cfg.LuaScript != "" {
		args = append(args, "--lua="+cfg.LuaScript)
	}

	cmd := exec.Command(exe, args...)
	env := os.Environ()
	env = append(env,
		fmt.Sprintf("BIZHAWK_IPC_PORT=%d", cfg.BizhawkIPCPort),
		fmt.Sprintf("BIZHAWK_ROM_DIR=%s", cfg.RomDir),
		fmt.Sprintf("BIZHAWK_SAVE_DIR=%s", cfg.SaveDir),
	)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Printf("Launching BizHawk: %s %v", exe, args)
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return cmd, nil
}
