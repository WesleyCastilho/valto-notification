package notification

import (
	"crypto/sha256"
	"errors"
	"net/url"
	"os"
	"strings"
	"time"
)

type Tenant struct {
	ID        string    `json:"id"`
	APIKey    string    `json:"api_key"`
	Evolution Evolution `json:"evolution"`
	TokenHash [32]byte  `json:"-"`
}
type Config struct {
	Port            string
	DatabaseURL     string
	Mode            string
	DeliveryEnabled bool
	Interval        time.Duration
	Tenants         []Tenant
}

func LoadConfig() (Config, error) {
	c := Config{Port: os.Getenv("PORT"), DatabaseURL: os.Getenv("DATABASE_URL"), Mode: os.Getenv("MODE"), DeliveryEnabled: os.Getenv("DELIVERY_ENABLED") == "true", Interval: time.Second}
	if c.Port == "" {
		c.Port = "8080"
	}
	if c.Mode == "" {
		c.Mode = "all"
	}
	if c.Mode != "all" && c.Mode != "api" && c.Mode != "worker" {
		return c, errors.New("MODE must be api, worker or all")
	}
	if c.DatabaseURL == "" || strings.Contains(c.DatabaseURL, "<") {
		return c, errors.New("DATABASE_URL must be a real PostgreSQL connection URL")
	}
	parsed, err := url.Parse(c.DatabaseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return c, errors.New("DATABASE_URL must be a valid PostgreSQL URL")
	}
	if value := os.Getenv("SEND_INTERVAL"); value != "" {
		c.Interval, err = time.ParseDuration(value)
		if err != nil || c.Interval < time.Second || c.Interval > time.Minute {
			return c, errors.New("SEND_INTERVAL must be between 1s and 1m")
		}
	}
	evolution := Evolution{
		URL: strings.TrimSpace(os.Getenv("EVOLUTION_API_URL")),
		APIKey: os.Getenv("EVOLUTION_API_KEY"),
		Instance: strings.TrimSpace(os.Getenv("EVOLUTION_INSTANCE_NAME")),
	}
	if evolution.URL == "" || evolution.APIKey == "" || evolution.Instance == "" {
		return c, errors.New("EVOLUTION_API_URL, EVOLUTION_API_KEY and EVOLUTION_INSTANCE_NAME are required")
	}
	clientID := os.Getenv("CLIENT_ID")
	if clientID == "" {
		clientID = "bunker"
	}
	clientKey := os.Getenv("VALTO_API_KEY")
	if clientKey == "" {
		clientKey = evolution.APIKey
	}
	c.Tenants = []Tenant{{ID: clientID, APIKey: clientKey, Evolution: evolution}}
	if err = validateTenants(c.Tenants); err != nil {
		return c, err
	}
	return c, nil
}

func validateTenants(tenants []Tenant) error {
	if len(tenants) == 0 {
		return errors.New("configure at least one client")
	}
	ids, keys := map[string]bool{}, map[string]bool{}
	for i := range tenants {
		t := &tenants[i]
		if len(t.ID) == 0 || len(t.ID) > 100 || len(t.APIKey) < 32 || ids[t.ID] || keys[t.APIKey] {
			return errors.New("clients need unique IDs and unique API keys with at least 32 characters")
		}
		for _, ch := range t.ID {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
				return errors.New("client ID must use lowercase letters, digits, hyphens or underscores")
			}
		}
		u, err := url.Parse(t.Evolution.URL)
		local := err == nil && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1")
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && local)) || t.Evolution.APIKey == "" || strings.TrimSpace(t.Evolution.Instance) == "" {
			return errors.New("each client requires a valid Evolution URL, key and instance name")
		}
		ids[t.ID] = true
		keys[t.APIKey] = true
		t.TokenHash = sha256.Sum256([]byte(t.APIKey))
	}
	return nil
}
