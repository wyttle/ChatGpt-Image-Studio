package main

import (
	"context"
	"testing"

	"chatgpt2api/internal/config"
	"chatgpt2api/internal/configstore"
)

func TestApplyEnvConfigOverridesSetsStorageBootstrap(t *testing.T) {
	cfg := config.New(t.TempDir())
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	t.Setenv("APP_AUTH_KEY", "env-auth")
	t.Setenv("STORAGE_BACKEND", "redis")
	t.Setenv("STORAGE_CONFIG_BACKEND", "redis")
	t.Setenv("STORAGE_IMAGE_CONVERSATION_STORAGE", "server")
	t.Setenv("STORAGE_IMAGE_DATA_STORAGE", "server")
	t.Setenv("REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("REDIS_PASSWORD", "123456")
	t.Setenv("REDIS_DB", "0")
	t.Setenv("REDIS_PREFIX", "chatgpt2api:test:env")

	if err := applyEnvConfigOverrides(cfg); err != nil {
		t.Fatalf("applyEnvConfigOverrides() returned error: %v", err)
	}

	if cfg.App.AuthKey != "env-auth" {
		t.Fatalf("AuthKey = %q, want env-auth", cfg.App.AuthKey)
	}
	if cfg.Storage.Backend != "redis" {
		t.Fatalf("Storage.Backend = %q, want redis", cfg.Storage.Backend)
	}
	if cfg.Storage.ConfigBackend != "redis" {
		t.Fatalf("Storage.ConfigBackend = %q, want redis", cfg.Storage.ConfigBackend)
	}
	if cfg.Storage.ImageConversationStorage != "server" || cfg.Storage.ImageDataStorage != "server" {
		t.Fatalf("expected image storage server/server, got %q/%q", cfg.Storage.ImageConversationStorage, cfg.Storage.ImageDataStorage)
	}
}

func TestLoadRedisConfigOverrides(t *testing.T) {
	cfg := config.New(t.TempDir())
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	cfg.Storage.ConfigBackend = "redis"
	cfg.Storage.RedisAddr = "127.0.0.1:6379"
	cfg.Storage.RedisPassword = "123456"
	cfg.Storage.RedisDB = 0
	cfg.Storage.RedisPrefix = "chatgpt2api:test:main"

	store := configstore.NewRedis(
		cfg.Storage.RedisAddr,
		cfg.Storage.RedisPassword,
		cfg.Storage.RedisDB,
		cfg.Storage.RedisPrefix,
	)
	defer store.Close()
	if err := store.Ping(context.Background()); err != nil {
		t.Skipf("redis backend is not reachable: %v", err)
	}
	if err := store.Save(context.Background(), map[string]map[string]any{
		"storage": {
			"backend":                    "redis",
			"config_backend":             "redis",
			"redis_addr":                 cfg.Storage.RedisAddr,
			"redis_password":             cfg.Storage.RedisPassword,
			"redis_db":                   cfg.Storage.RedisDB,
			"redis_prefix":               cfg.Storage.RedisPrefix,
			"image_conversation_storage": "server",
			"image_data_storage":         "server",
		},
		"app": {
			"auth_key": "redis-auth",
		},
	}); err != nil {
		t.Fatalf("store.Save() returned error: %v", err)
	}

	if err := loadRedisConfigOverrides(context.Background(), cfg); err != nil {
		t.Fatalf("loadRedisConfigOverrides() returned error: %v", err)
	}
	if cfg.App.AuthKey != "redis-auth" {
		t.Fatalf("AuthKey = %q, want redis-auth", cfg.App.AuthKey)
	}
	if cfg.Storage.ImageConversationStorage != "server" || cfg.Storage.ImageDataStorage != "server" {
		t.Fatalf("expected image storage server/server, got %q/%q", cfg.Storage.ImageConversationStorage, cfg.Storage.ImageDataStorage)
	}
}
