package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"syscall"

	"github.com/anthropics/feishu-codex-bridge/bridge"
	"github.com/joho/godotenv"
)

func main() {
	workDirFlag := flag.String("workdir", "", "Working directory for Codex (overrides WORKING_DIR)")
	flag.Parse()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to determine home directory: %v", err)
	}

	configDir := filepath.Join(homeDir, ".feishu-codex-bridge")
	defaultEnvPath := filepath.Join(configDir, ".env")

	if err := os.MkdirAll(configDir, 0o700); err != nil {
		log.Fatalf("Failed to create config directory %s: %v", configDir, err)
	}

	lockFile, err := acquireSingleInstanceLock(configDir)
	if err != nil {
		var instErr *SingleInstanceError
		if errors.As(err, &instErr) && instErr.PID > 0 {
			fmt.Printf("❌ 已有实例在运行（PID=%d），本程序只允许单实例运行。\n", instErr.PID)
			fmt.Printf("请手动停止后再重试，例如：\n")
			fmt.Printf("  kill -TERM %d\n", instErr.PID)
			fmt.Printf("  # 若仍未退出：kill -KILL %d\n", instErr.PID)
		} else {
			fmt.Printf("❌ %v\n", err)
			fmt.Println("提示：本程序只允许单实例运行；请手动停止正在运行的实例后再重试。")
		}
		os.Exit(3)
	}
	defer lockFile.Close()

	// Snapshot environment before loading any file-based configs.
	// We never override already-exported environment variables.
	envPreexisting := map[string]struct{}{}
	for _, kv := range os.Environ() {
		// "KEY=VALUE"
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				envPreexisting[kv[:i]] = struct{}{}
				break
			}
		}
	}

	// Ensure default env exists so binary can run from any directory.
	_, envStatErr := os.Stat(defaultEnvPath)
	envMissing := os.IsNotExist(envStatErr)
	if envMissing {
		if err := os.WriteFile(defaultEnvPath, []byte(envExample), 0o600); err != nil {
			log.Fatalf("Failed to write default env file %s: %v", defaultEnvPath, err)
		}
		fmt.Printf("Created default config: %s (please edit it). You can also create <workdir>/.feishu-codex-bridge/.env to override per project.\n", defaultEnvPath)
	}

	applyEnvFile := func(path string, overrideExisting bool) {
		m, err := godotenv.Read(path)
		if err != nil {
			return
		}
		for k, v := range m {
			// Never override real environment variables that were already present
			// when the process started.
			if _, ok := envPreexisting[k]; ok {
				continue
			}
			if !overrideExisting {
				if _, exists := os.LookupEnv(k); exists {
					continue
				}
			}
			_ = os.Setenv(k, v)
		}
	}

	// Load global default env first (does not override real environment variables).
	applyEnvFile(defaultEnvPath, false)

	// Resolve effective working directory (used for per-project overrides).
	effectiveWorkDir := ""
	if *workDirFlag != "" {
		effectiveWorkDir = *workDirFlag
	} else if val := os.Getenv("WORKING_DIR"); val != "" {
		effectiveWorkDir = val
	} else {
		effectiveWorkDir = "."
	}

	perProjectEnvPath := filepath.Join(filepath.Clean(effectiveWorkDir), ".feishu-codex-bridge", ".env")
	if _, err := os.Stat(perProjectEnvPath); err == nil {
		// Per-project env should override the global default, but still must not
		// override environment variables exported before the process started.
		applyEnvFile(perProjectEnvPath, true)
	}

	// If required secrets are missing, exit early (do not start Codex).
	if os.Getenv("FEISHU_APP_ID") == "" || os.Getenv("FEISHU_APP_SECRET") == "" {
		if envMissing {
			fmt.Printf("Missing required config. Please edit %s and set FEISHU_APP_ID and FEISHU_APP_SECRET, then re-run.\n", defaultEnvPath)
			fmt.Printf("Optional per-project override: %s\n", perProjectEnvPath)
		} else {
			fmt.Printf("Missing required config. Set FEISHU_APP_ID and FEISHU_APP_SECRET (or edit %s), then re-run.\n", defaultEnvPath)
			fmt.Printf("Optional per-project override: %s\n", perProjectEnvPath)
		}
		os.Exit(2)
	}

	// Parse session config
	sessionIdleMin := 60 // default 60 minutes
	if val := os.Getenv("SESSION_IDLE_MINUTES"); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			sessionIdleMin = parsed
		}
	}

	sessionResetHr := 4 // default 4 AM
	if val := os.Getenv("SESSION_RESET_HOUR"); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			sessionResetHr = parsed
		}
	}

	// Session DB path
	sessionDBPath := os.Getenv("SESSION_DB_PATH")
	if sessionDBPath == "" {
		newDefault := filepath.Join(configDir, "sessions.db")
		legacyDefault := filepath.Join(homeDir, ".feishu-codex", "sessions.db")

		if _, err := os.Stat(newDefault); err == nil {
			sessionDBPath = newDefault
		} else if _, err := os.Stat(legacyDefault); err == nil {
			sessionDBPath = legacyDefault
		} else {
			sessionDBPath = newDefault
		}
	}

	config := bridge.Config{
		FeishuAppID:     os.Getenv("FEISHU_APP_ID"),
		FeishuAppSecret: os.Getenv("FEISHU_APP_SECRET"),
		WorkingDir:      os.Getenv("WORKING_DIR"),
		CodexModel:      os.Getenv("CODEX_MODEL"),
		SessionDBPath:   sessionDBPath,
		SessionIdleMin:  sessionIdleMin,
		SessionResetHr:  sessionResetHr,
		Debug:           os.Getenv("DEBUG") == "true",
	}

	if config.FeishuAppID == "" || config.FeishuAppSecret == "" {
		log.Fatal("FEISHU_APP_ID and FEISHU_APP_SECRET are required")
	}

	if config.WorkingDir == "" {
		if *workDirFlag != "" {
			config.WorkingDir = *workDirFlag
		} else {
			config.WorkingDir = "."
		}
	} else if *workDirFlag != "" {
		config.WorkingDir = *workDirFlag
	}

	b, err := bridge.New(config)
	if err != nil {
		log.Fatalf("Failed to create bridge: %v", err)
	}

	// 优雅退出
	var shuttingDown int32
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		atomic.StoreInt32(&shuttingDown, 1)
		fmt.Println("\nShutting down...")
		b.Stop()
	}()

	fmt.Println("Starting Feishu-Codex Bridge (ACP mode)...")
	if err := b.Start(); err != nil && atomic.LoadInt32(&shuttingDown) == 0 && !errors.Is(err, context.Canceled) {
		log.Fatalf("Bridge error: %v", err)
	} else if err != nil {
		log.Printf("Bridge stopped: %v", err)
	}
}
