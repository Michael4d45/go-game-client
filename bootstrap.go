package main

import (
	"archive/zip"
	"bufio"
	"context"
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
	if err := os.MkdirAll(cfg.RomDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.SaveDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll("scripts", 0o755); err != nil {
		return err
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

	api := NewAPI(cfg)
	reader := bufio.NewReader(os.Stdin)
	ctx := context.Background()

	// --- Player registration / token check ---
	for {
		if cfg.PlayerName == "" || cfg.BearerToken == "" || cfg.AppKey == "" {
			fmt.Print("Enter your desired player ID: ")
			playerName, _ := reader.ReadString('\n')
			cfg.PlayerName = strings.TrimSpace(playerName)

			token, appKey, err := api.RegisterPlayer(ctx, cfg.PlayerName)
			if err != nil {
				log.Printf("registerPlayer failed: %v", err)
				fmt.Println("failed to register player")
				continue
			}
			cfg.BearerToken = token
			cfg.AppKey = appKey
			api = NewAPI(cfg) // refresh API with new bearer
			break
		}

		ok, err := api.CheckTokenExists(ctx, cfg.BearerToken)
		if err != nil {
			log.Printf("check token failed: %v", err)
			cfg.BearerToken = ""
			cfg.AppKey = ""
			continue
		}
		if ok {
			break
		}
		cfg.BearerToken = ""
		cfg.AppKey = ""
	}

	// --- Session join ---
	for {
		if cfg.SessionName != "" {
			exists, err := api.CheckSessionExists(ctx, cfg.SessionName)
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
		cfg.SessionName = strings.TrimSpace(sessionName)
	}

	games, err := api.JoinSession(ctx, cfg.SessionName)
	if err != nil {
		return err
	}

	// --- Download missing games ---
	var wg sync.WaitGroup
	errCh := make(chan error, len(games))

	for _, g := range games {
		localPath := filepath.Join(cfg.RomDir, g)

		if _, err := os.Stat(localPath); err == nil {
			fmt.Println("Game already exists:", g)
			continue
		}

		wg.Add(1)
		go func(gameFile, dest string) {
			defer wg.Done()
			fmt.Println("Downloading:", gameFile)
			romURL := cfg.ServerURL + "/api/roms/" + gameFile
			if err := DownloadFile(romURL, dest); err != nil {
				log.Printf("Failed to download %s: %v", gameFile, err)
				errCh <- err
			}
		}(g, localPath)
	}

	wg.Wait()
	close(errCh)
	for e := range errCh {
		if e != nil {
			return e
		}
	}

	// --- Download latest Lua script ---
	luaURL := cfg.ServerURL + "/api/scripts/latest"
	luaDest := filepath.Join("scripts", "swap_latest.lua")
	if err := DownloadFile(luaURL, luaDest); err != nil {
		return err
	}
	cfg.LuaScript = luaDest

	// Save updated config
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
			if err := os.MkdirAll(fpath, f.Mode()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fpath), 0o755); err != nil {
			return err
		}
		outFile, err := os.OpenFile(
			fpath,
			os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
			f.Mode(),
		)
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
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

// DownloadFile streams the URL to dest (overwrites dest).
func DownloadFile(url, dest string) error {
	log.Printf("DownloadFile: %s -> %s", url, dest)

	// ensure parent dir exists
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}

	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}
