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

// Bootstrap handles the initial setup, including downloading assets,
// registering the player, and joining a session.
func Bootstrap(cfg *Config) error {
	if err := createDirectories(cfg); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	if err := ensureBizHawkInstalled(cfg); err != nil {
		return fmt.Errorf("BizHawk installation check failed: %w", err)
	}

	api := NewAPI(cfg)
	ctx := context.Background()

	if err := ensurePlayerRegistered(ctx, cfg, api); err != nil {
		return fmt.Errorf("player registration failed: %w", err)
	}
	// The bearer token might have been updated, so create a new API client.
	api = NewAPI(cfg)

	if err := ensureSessionJoined(ctx, cfg, api); err != nil {
		return fmt.Errorf("session join failed: %w", err)
	}

	games, err := api.JoinSession(ctx, cfg.SessionName)
	if err != nil {
		return fmt.Errorf("failed to get game list from session: %w", err)
	}

	if err := downloadMissingGames(cfg, games); err != nil {
		return fmt.Errorf("failed to download games: %w", err)
	}

	if err := downloadLatestLuaScript(cfg); err != nil {
		return fmt.Errorf("failed to download lua script: %w", err)
	}

	return SaveConfig(cfg, "config.json")
}

func createDirectories(cfg *Config) error {
	dirs := []string{cfg.RomDir, cfg.SaveDir, "scripts"}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func ensureBizHawkInstalled(cfg *Config) error {
	zipFileName := filepath.Base(cfg.BizHawkDownloadURL)
	installDir := strings.TrimSuffix(zipFileName, filepath.Ext(zipFileName))
	cfg.BizHawkPath = filepath.Join(installDir, "EmuHawk.exe")

	if _, err := os.Stat(cfg.BizHawkPath); os.IsNotExist(err) {
		fmt.Println("BizHawk not found. Downloading...")
		if err := DownloadAndExtract(
			httpClient,
			cfg.BizHawkDownloadURL,
			zipFileName,
			installDir,
		); err != nil {
			return err
		}
		fmt.Println("BizHawk installed in", installDir)

		bizhawkFilesURL := cfg.ServerURL + "/api/BizhawkFiles.zip"
		fmt.Println("Downloading BizhawkFiles.zip...")
		if err := DownloadAndExtract(
			httpClient,
			bizhawkFilesURL,
			"BizhawkFiles.zip",
			installDir,
		); err != nil {
			return fmt.Errorf(
				"failed to download and extract BizhawkFiles.zip: %w",
				err,
			)
		}
		fmt.Println("BizhawkFiles.zip extracted into BizHawk directory.")
	}
	return nil
}

func ensurePlayerRegistered(ctx context.Context, cfg *Config, api *API) error {
	reader := bufio.NewReader(os.Stdin)
	for {
		if cfg.BearerToken != "" {
			ok, err := api.CheckTokenExists(ctx, cfg.BearerToken)
			if err != nil {
				log.Printf("Token check failed, re-registering: %v", err)
				cfg.BearerToken, cfg.AppKey = "", ""
				continue // Retry
			}
			if ok {
				return nil // Token is valid
			}
			log.Println("Bearer token is invalid, re-registering.")
			cfg.BearerToken, cfg.AppKey = "", ""
		}

		fmt.Print("Enter your desired player ID: ")
		playerName, _ := reader.ReadString('\n')
		cfg.PlayerName = strings.TrimSpace(playerName)

		token, appKey, err := api.RegisterPlayer(ctx, cfg.PlayerName)
		if err != nil {
			log.Printf("RegisterPlayer failed: %v", err)
			fmt.Println("Failed to register player. Please try again.")
			continue
		}
		cfg.BearerToken = token
		cfg.AppKey = appKey
		return nil
	}
}

func ensureSessionJoined(ctx context.Context, cfg *Config, api *API) error {
	reader := bufio.NewReader(os.Stdin)
	for {
		if cfg.SessionName != "" {
			exists, err := api.CheckSessionExists(ctx, cfg.SessionName)
			if err != nil {
				return err
			}
			if exists {
				return nil // Session exists
			}
			log.Printf("Session '%s' not found.", cfg.SessionName)
			cfg.SessionName = ""
		}

		fmt.Print("Enter game session name: ")
		sessionName, _ := reader.ReadString('\n')
		cfg.SessionName = strings.TrimSpace(sessionName)
	}
}

func downloadMissingGames(cfg *Config, games []string) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(games))

	for _, g := range games {
		localPath := filepath.Join(cfg.RomDir, g)
		if _, err := os.Stat(localPath); err == nil {
			log.Println("Game already exists:", g)
			continue
		}

		wg.Add(1)
		go func(gameFile, dest string) {
			defer wg.Done()
			log.Println("Downloading:", gameFile)
			romURL := cfg.ServerURL + "/api/roms/" + gameFile
			if err := DownloadFile(httpClient, romURL, dest); err != nil {
				err := fmt.Errorf("failed to download %s: %w", gameFile, err)
				log.Print(err)
				errCh <- err
			}
		}(g, localPath)
	}

	wg.Wait()
	close(errCh)

	// Return the first error encountered, if any.
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func downloadLatestLuaScript(cfg *Config) error {
	luaURL := cfg.ServerURL + "/api/scripts/latest"
	luaDest := filepath.Join("scripts", "swap_latest.lua")
	if err := DownloadFile(httpClient, luaURL, luaDest); err != nil {
		return err
	}
	cfg.LuaScript = luaDest
	return nil
}

func DownloadAndExtract(
	client *http.Client,
	url,
	zipPath,
	dest string,
) error {
	if err := DownloadFile(client, url, zipPath); err != nil {
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
		if !strings.HasPrefix(
			fpath,
			filepath.Clean(dest)+string(os.PathSeparator),
		) {
			return fmt.Errorf("illegal file path: %s", fpath)
		}

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

// DownloadFile streams the URL to dest.
func DownloadFile(client *http.Client, url, dest string) error {
	log.Printf("DownloadFile: %s -> %s", url, dest)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}

	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s (status: %s)", url, resp.Status)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}
