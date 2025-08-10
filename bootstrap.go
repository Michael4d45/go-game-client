package main

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func Bootstrap(cfg *Config) error {
	os.MkdirAll(cfg.RomDir, 0755)
	os.MkdirAll(cfg.SaveDir, 0755)
	os.MkdirAll("scripts", 0755)

	if cfg.BizHawkDownloadURL == "" {
		cfg.BizHawkDownloadURL = "https://github.com/TASEmulators/BizHawk/releases/download/2.10/BizHawk-2.10-win-x64.zip"
	}

	zipFileName := filepath.Base(cfg.BizHawkDownloadURL)
	installDir := strings.TrimSuffix(zipFileName, filepath.Ext(zipFileName))
	cfg.BizHawkPath = filepath.Join(installDir, "EmuHawk.exe")

	if _, err := os.Stat(cfg.BizHawkPath); os.IsNotExist(err) {
		fmt.Println("BizHawk not found. Downloading...")
		if err := DownloadAndExtract(cfg.BizHawkDownloadURL, zipFileName, installDir); err != nil {
			return err
		}
		fmt.Println("BizHawk installed in", installDir)
	}

	reader := bufio.NewReader(os.Stdin)

	for {
		if cfg.PlayerID == "" || cfg.Reverb.BearerToken == "" || cfg.Reverb.AppKey == "" {
			fmt.Print("Enter your desired player ID: ")
			playerID, _ := reader.ReadString('\n')
			cfg.PlayerID = strings.TrimSpace(playerID)

			token, appKey, authURL, err := registerPlayer(cfg.ServerURL, cfg.PlayerID)
			if err != nil {
				log.Println(fmt.Errorf("%w", err))
				fmt.Println("failed to register player")
				continue
			}
			cfg.Reverb.BearerToken = token
			cfg.Reverb.AppKey = appKey
			cfg.Reverb.AuthURL = authURL
			break
		}

		if (!checkTokenExists(cfg.ServerURL, cfg.Reverb.BearerToken)) {
			cfg.Reverb.BearerToken = ""
			cfg.Reverb.AppKey = ""
			cfg.Reverb.AuthURL = ""
		}
	}

	for {
		if cfg.SessionName != "" {
			exists, err := checkSessionExists(cfg.ServerURL, cfg.SessionName, cfg.Reverb.BearerToken)
			if err != nil {
				return err
			}
			if exists {
				break
			}
			cfg.SessionName = ""
		}

		fmt.Print("Enter game session name: ")
		sessionName, _ := reader.ReadString('\n')
		sessionName = strings.TrimSpace(sessionName)

		exists, err := checkSessionExists(cfg.ServerURL, sessionName, cfg.Reverb.BearerToken)
		if err != nil {
			return err
		}
		if exists {
			cfg.SessionName = sessionName
			break
		}
	}

	joinSession(cfg.ServerURL, cfg.SessionName, cfg.Reverb.BearerToken)

	luaURL := cfg.ServerURL + "/api/scripts/latest"
	luaDest := filepath.Join("scripts", "swap_latest.lua")
	if err := DownloadFile(luaURL, luaDest); err != nil {
		return err
	}
	cfg.LuaScript = luaDest

	return SaveConfig(cfg, "config.json")
}

func DownloadAndExtract(url, zipPath, dest string) error {
	if err := DownloadFile(url, zipPath); err != nil {
		return err
	}
	defer os.Remove(zipPath)

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, f.Mode())
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
			return err
		}
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func registerPlayer(serverURL, playerID string) (string, string, string, error) {
	reqBody := strings.NewReader(fmt.Sprintf(`{"player_id":"%s"}`, playerID))
	resp, err := http.Post(serverURL+"/api/register-player", "application/json", reqBody)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("server returned %s", resp.Status)
	}

	var data struct {
		PlayerID      string `json:"player_id"`
		BearerToken   string `json:"bearer_token"`
		ReverbAppKey  string `json:"reverb_app_key"`
		ReverbAuthURL string `json:"reverb_auth_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", "", "", err
	}

	return data.BearerToken, data.ReverbAppKey, data.ReverbAuthURL, nil
}

func checkSessionExists(serverURL, sessionName, token string) (bool, error) {
	req, err := http.NewRequest("GET", serverURL+"/api/check-session/"+sessionName, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("server returned %s", resp.Status)
	}
	return true, nil
}

func joinSession(serverURL, sessionName, token string) {
	req, err := http.NewRequest("POST", serverURL+"/api/join-session/"+sessionName, nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	http.DefaultClient.Do(req)
}

func checkTokenExists(serverURL, token string) (bool) {
	req, err := http.NewRequest("GET", serverURL+"/api/check-token", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return false
	}
	if resp.StatusCode != http.StatusOK {
		return false
	}
	return true
}