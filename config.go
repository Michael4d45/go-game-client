package main

import (
	"encoding/json"
	"os"
)

type ReverbConfig struct {
	AppKey      string `json:"app_key"`
	AuthURL     string `json:"auth_url"`
	BearerToken string `json:"bearer_token"`
	HostPort    int    `json:"host_port"`
}

type Config struct {
	ServerURL          string       `json:"server_url"`
	Reverb             ReverbConfig `json:"reverb"`
	PlayerID           string       `json:"player_id"`
	SessionName        string       `json:"session_name"`
	BizHawkDownloadURL string       `json:"bizhawk_download_url"`
	BizHawkPath        string       `json:"bizhawk_path"`
	LuaScript          string       `json:"lua_script"`
	RomDir             string       `json:"rom_dir"`
	SaveDir            string       `json:"save_dir"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		ServerURL: "http://bizhawk-shuffler-server.test",
		Reverb: ReverbConfig{
			AppKey:      "",
			AuthURL:     "",
			BearerToken: "",
			HostPort:    8080,
		},
		PlayerID:           "",
		SessionName:        "",
		BizHawkDownloadURL: "https://github.com/TASEmulators/BizHawk/releases/download/2.10/BizHawk-2.10-win-x64.zip",
		BizHawkPath:        "BizHawk-2.10-win-x64\\EmuHawk.exe",
		LuaScript:          "scripts\\swap_latest.lua",
		RomDir:             "roms",
		SaveDir:            "saves",
	}
}

func LoadOrCreateConfig(path string) (*Config, error) {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Write default config
		defaultCfg := DefaultConfig()
		if err := SaveConfig(defaultCfg, path); err != nil {
			return nil, err
		}
		return defaultCfg, nil
	}

	// Otherwise load existing config
	return LoadConfig(path)
}

func LoadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	err = json.NewDecoder(f).Decode(&cfg)
	return &cfg, err
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
