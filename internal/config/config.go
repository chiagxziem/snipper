package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Env                string
	Port               string
	FrontendURL        string
	DatabaseURL        string
	RedisURL           string
	CORSAllowedOrigin  string
	ResendAPIKey       string
	ResendDomain       string
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURI  string
	TicketSecret       string
}

const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

func (c *Config) validate() error {
	required := map[string]string{
		"ENV":                  c.Env,
		"FRONTEND_URL":         c.FrontendURL,
		"DATABASE_URL":         c.DatabaseURL,
		"REDIS_URL":            c.RedisURL,
		"CORS_ALLOWED_ORIGIN":  c.CORSAllowedOrigin,
		"RESEND_API_KEY":       c.ResendAPIKey,
		"RESEND_DOMAIN":        c.ResendDomain,
		"GOOGLE_CLIENT_ID":     c.GoogleClientID,
		"GOOGLE_CLIENT_SECRET": c.GoogleClientSecret,
		"GOOGLE_REDIRECT_URI":  c.GoogleRedirectURI,
		"TICKET_SECRET":        c.TicketSecret,
	}

	for k, v := range required {
		if v == "" {
			return fmt.Errorf("config: missing required environment variable: %s", k)
		}
	}

	if c.Port == "" {
		c.Port = "8080"
	}

	return nil
}

func Load() (*Config, error) {
	godotenv.Load()

	cfg := &Config{
		Env:                os.Getenv("ENV"),
		Port:               os.Getenv("PORT"),
		FrontendURL:        os.Getenv("FRONTEND_URL"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		RedisURL:           os.Getenv("REDIS_URL"),
		CORSAllowedOrigin:  os.Getenv("CORS_ALLOWED_ORIGIN"),
		ResendAPIKey:       os.Getenv("RESEND_API_KEY"),
		ResendDomain:       os.Getenv("RESEND_DOMAIN"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURI:  os.Getenv("GOOGLE_REDIRECT_URI"),
		TicketSecret:       os.Getenv("TICKET_SECRET"),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) IsProduction() bool {
	return c.Env == EnvProduction
}
