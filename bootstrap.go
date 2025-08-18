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
	"sync"
)

func Bootstrap(cfg *Config) error {
	os.MkdirAll(cfg.RomDir, 0755)
	os.MkdirAll(cfg.SaveDir, 0755)
	os.MkdirAll("scripts", 0755)

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
		if cfg.PlayerName == "" || cfg.BearerToken == "" || cfg.AppKey == "" {
			fmt.Print("Enter your desired player ID: ")
			playerName, _ := reader.ReadString('\n')
			cfg.PlayerName = strings.TrimSpace(playerName)

			token, appKey, err := registerPlayer(cfg.ServerURL, cfg.PlayerName)
			if err != nil {
				log.Println(fmt.Errorf("%w", err))
				fmt.Println("failed to register player")
				continue
			}
			cfg.BearerToken = token
			cfg.AppKey = appKey
			break
		}

		if checkTokenExists(cfg.ServerURL, cfg.BearerToken) {
			break
		} else {
			log.Println("check token failed")
			cfg.BearerToken = ""
			cfg.AppKey = ""
		}
	}

	for {
		if cfg.SessionName != "" {
			exists, err := checkSessionExists(cfg.ServerURL, cfg.SessionName, cfg.BearerToken)
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
		cfg.SessionName = sessionName
	}

	games, err := joinSession(cfg.ServerURL, cfg.SessionName, cfg.BearerToken)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(games)) // collect errors

	for _, g := range games {
		localPath := filepath.Join(cfg.RomDir, g)

		// Check if already exists
		if _, err := os.Stat(localPath); err == nil {
			fmt.Println("Game already exists:", g)
			continue
		}

		// Missing → download in parallel
		wg.Add(1)
		go func(gameFile string, dest string) {
			defer wg.Done()
			fmt.Println("Downloading:", gameFile)
			romURL := cfg.ServerURL + "/api/roms/" + gameFile
			if err := DownloadFile(romURL, dest); err != nil {
				log.Printf("Failed to download %s: %v", gameFile, err)
				errCh <- err
			}
		}(g, localPath)
	}

	// Wait for all downloads
	wg.Wait()
	close(errCh)

	// If any errors occurred, return the first one
	for e := range errCh {
		if e != nil {
			return e
		}
	}

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

func registerPlayer(serverURL, playerName string) (string, string, error) {
	// Prepare request body
	reqBody := strings.NewReader(fmt.Sprintf(`{"name":"%s"}`, playerName))

	log.Println("Registering player: " + playerName)

	// Create a new request
	req, err := http.NewRequest("POST", serverURL+"/api/register-player", reqBody)
	if err != nil {
		return "", "", err
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	// Check status
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("server returned %s", resp.Status)
	}

	// Decode JSON response
	var data struct {
		BearerToken  string `json:"bearer_token"`
		ReverbAppKey string `json:"reverb_app_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", "", err
	}

	return data.BearerToken, data.ReverbAppKey, nil
}

func checkSessionExists(serverURL, sessionName, token string) (bool, error) {
	req, err := http.NewRequest("GET", serverURL+"/api/check-session/"+sessionName, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

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

type Game struct {
	File string `json:"file"`
}

type Session struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Games []Game `json:"games"`
}

func joinSession(serverURL, sessionName, token string) ([]string, error) {
	req, err := http.NewRequest("POST", serverURL+"/api/join-session/"+sessionName, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %s", resp.Status)
	}

	var session Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, err
	}

	// Extract just the file names
	var gameFiles []string
	for _, g := range session.Games {
		gameFiles = append(gameFiles, g.File)
	}

	return gameFiles, nil
}

func checkTokenExists(serverURL, token string) bool {
	req, err := http.NewRequest("POST", serverURL+"/api/check-token", nil)
	if err != nil {
		log.Printf("checkTokenExists: failed to create request: %v", err)
		return false
	}

	// Set Authorization header
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	// Log request info (mask token for safety)
	maskedToken := "<empty>"
	if len(token) > 8 {
		maskedToken = token[:6] + "..."
	} else if len(token) > 0 {
		maskedToken = "..."
	}
	log.Printf(
		"checkTokenExists: sending request: method=%s url=%s token=%s",
		req.Method,
		req.URL.String(),
		maskedToken,
	)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("checkTokenExists: request failed: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		log.Printf("checkTokenExists: token not found (404)")
		return false
	}
	if resp.StatusCode != http.StatusOK {
		log.Printf("checkTokenExists: unexpected status code %d", resp.StatusCode)
		return false
	}

	log.Printf("checkTokenExists: token is valid")
	return true
}
