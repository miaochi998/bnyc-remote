// Package config loads runtime configuration exclusively from environment
// variables. Rule R1: no secrets or real values are baked into the binary or
// image; missing critical variables cause startup to fail.
package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// Config holds all runtime settings.
type Config struct {
	Listen       string
	DBPath       string
	JWTSecret    []byte
	DeviceKey    []byte
	AdminUser    string
	AdminPass    string
	TokenTTLHrs  int
	AllowOrigins string
}

// Load reads and validates configuration from the environment.
func Load() (*Config, error) {
	c := &Config{
		Listen:       env("PORTAL_LISTEN", ":8080"),
		DBPath:       env("PORTAL_DB_PATH", "/data/portal.db"),
		AdminUser:    os.Getenv("PORTAL_ADMIN_USER"),
		AdminPass:    os.Getenv("PORTAL_ADMIN_PASSWORD"),
		AllowOrigins: os.Getenv("PORTAL_ALLOW_ORIGINS"),
		TokenTTLHrs:  12,
	}

	jwtSecret := os.Getenv("PORTAL_JWT_SECRET")
	if len(jwtSecret) < 32 {
		return nil, fmt.Errorf("PORTAL_JWT_SECRET must be at least 32 characters")
	}
	c.JWTSecret = []byte(jwtSecret)

	deviceKeyHex := strings.TrimSpace(os.Getenv("PORTAL_DEVICE_KEY"))
	if deviceKeyHex == "" {
		return nil, fmt.Errorf("PORTAL_DEVICE_KEY is required (64 hex chars = 32 bytes)")
	}
	key, err := hex.DecodeString(deviceKeyHex)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("PORTAL_DEVICE_KEY must be 64 hex chars (32 bytes)")
	}
	c.DeviceKey = key

	return c, nil
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
