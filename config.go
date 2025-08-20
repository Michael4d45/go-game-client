package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	AppKey      string `json:"app_key"`
	BearerToken string `json:"bearer_token"`

	ServerScheme string `json:"server_scheme"`
	ServerHost   string `json:"server_host"`
	ServerPort   int    `json:"server_port"`

	PusherPort int `json:"pusher_port"`

	PlayerName  string `json:"player_name"`
	SessionName string `json:"session_name"`

	BizHawkDownloadURL string `json:"bizhawk_download_url"`
	BizHawkPath        string `json:"bizhawk_path"`
	LuaScript          string `json:"lua_script"`
	RomDir             string `json:"rom_dir"`
	SaveDir            string `json:"save_dir"`

	BizhawkIPCPort int `json:"bizhawk_ipc_port"`

	// Computed
	ServerURL string `json:"-"`
}

func (c *Config) ComputeURLs() {
	c.ServerURL = fmt.Sprintf("%s://%s:%d", c.ServerScheme, c.ServerHost, c.ServerPort)
}

func DefaultConfig() *Config {
	cfg := &Config{
		AppKey:      "",
		BearerToken: "",

		ServerScheme: "http",
		ServerHost:   "bizhawk-shuffler-server.test",
		ServerPort:   8080,

		PusherPort: 8000,

		PlayerName:  "",
		SessionName: "",

		BizHawkDownloadURL: "https://github.com/TASEmulators/BizHawk/releases/download/2.10/BizHawk-2.10-win-x64.zip",
		BizHawkPath:        "BizHawk-2.10-win-x64\\EmuHawk.exe",
		LuaScript:          "scripts\\swap_latest.lua",
		RomDir:             "roms",
		SaveDir:            "saves",

		BizhawkIPCPort: 55355,
	}
	cfg.ComputeURLs()
	return cfg
}

func LoadOrCreateConfig(path string) (*Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := DefaultConfig()
		if err := SaveConfig(cfg, path); err != nil {
			return nil, err
		}
		return cfg, nil
	}
	return LoadConfig(path)
}

func LoadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}

	if cfg.BizhawkIPCPort == 0 {
		cfg.BizhawkIPCPort = 55355
	}

	cfg.ComputeURLs()
	return &cfg, nil
}

func SaveConfig(cfg *Config, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(cfg)
}
