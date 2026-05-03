package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	MongoURI   string
	MongoDB    string
	Port       string
	JWTSecret  string
	JWTExpiry  time.Duration
	AdminUser  string
	AdminPass  string
}

func Load() *Config {
	expiry := 24 * time.Hour
	if raw := os.Getenv("JWT_EXPIRY_HOURS"); raw != "" {
		if h, err := strconv.Atoi(raw); err == nil {
			expiry = time.Duration(h) * time.Hour
		}
	}

	return &Config{
		MongoURI:  getEnv("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:   getEnv("MONGO_DB", "swallow"),
		Port:      getEnv("PORT", "3000"),
		JWTSecret: getEnv("JWT_SECRET", "changeme-in-production"),
		JWTExpiry: expiry,
		AdminUser: getEnv("BOOTSTRAP_ADMIN_USERNAME", "admin"),
		AdminPass: getEnv("BOOTSTRAP_ADMIN_PASSWORD", "admin"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
