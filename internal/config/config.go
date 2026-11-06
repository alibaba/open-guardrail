// Package config loads and validates deployment settings before opening storage.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"go.yaml.in/yaml/v3"
)

type Config struct {
	PublicURL string   `yaml:"public_url"`
	Listen    string   `yaml:"listen"`
	DataDir   string   `yaml:"data_dir"`
	Database  Database `yaml:"database"`
}
type Database struct {
	Driver         string `yaml:"driver"`
	URL            string `yaml:"url"`
	MaxConnections int    `yaml:"max_connections"`
	Migrations     string `yaml:"migrations"`
}

const StartupTimeout = 30 * time.Second

const maxConfigBytes = 1 << 20

var driverName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// Load applies defaults, YAML, environment variables, then explicit flags.
// DATABASE_URL never silently changes the selected backend.
func Load(path, listen, dataDir string) (Config, error) {
	c := Config{Listen: "127.0.0.1:8080", DataDir: "data", Database: Database{Driver: "sqlite", MaxConnections: 10, Migrations: "auto"}}
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return c, errors.New("cannot open configuration file")
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
		if err != nil {
			return c, errors.New("cannot read configuration file")
		}
		if len(data) > maxConfigBytes {
			return c, errors.New("configuration file exceeds 1 MiB limit")
		}
		d := yaml.NewDecoder(bytes.NewReader(data))
		d.KnownFields(true)
		if err := d.Decode(&c); err != nil {
			return c, errors.New("invalid YAML configuration (check fields and types)")
		}
		var extra any
		if err := d.Decode(&extra); err != io.EOF {
			return c, errors.New("configuration must contain exactly one YAML document")
		}
	}
	for key, target := range map[string]*string{
		"OPEN_GUARDRAIL_PUBLIC_URL": &c.PublicURL,
		"OPEN_GUARDRAIL_LISTEN":     &c.Listen, "OPEN_GUARDRAIL_DATA_DIR": &c.DataDir,
		"OPEN_GUARDRAIL_DATABASE_DRIVER": &c.Database.Driver, "DATABASE_URL": &c.Database.URL,
		"OPEN_GUARDRAIL_DATABASE_MIGRATIONS": &c.Database.Migrations,
	} {
		if value, ok := os.LookupEnv(key); ok {
			*target = value
		}
	}
	if listen != "" {
		c.Listen = listen
	}
	if dataDir != "" {
		c.DataDir = dataDir
	}
	// Driver-specific support and credential rules belong to the storage
	// registry. Configuration loading validates only the portable name syntax.
	if !driverName.MatchString(c.Database.Driver) {
		return c, errors.New("database.driver must be a valid driver name")
	}
	if c.Database.MaxConnections < 1 || c.Database.MaxConnections > 100 {
		return c, errors.New("database.max_connections must be between 1 and 100")
	}
	if c.Database.Migrations != "auto" && c.Database.Migrations != "external" {
		return c, errors.New("database.migrations must be auto or external")
	}
	if c.DataDir == "" {
		return c, errors.New("data_dir must not be empty")
	}
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return c, errors.New("listen must be a host:port address")
	}
	// Network exposure is explicit; identity traffic outside loopback requires
	// a configured HTTPS origin and a trusted reverse proxy.
	if host == "" {
		return c, errors.New("listen must include an explicit host")
	}
	if c.PublicURL != "" {
		u, err := url.Parse(c.PublicURL)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Scheme != "https" {
			return c, errors.New("public_url must be an HTTPS origin without path, credentials, query or fragment")
		}
	} else if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return c, errors.New("non-loopback listening requires an explicit HTTPS public_url and trusted TLS reverse proxy")
	}
	c.DataDir, err = filepath.Abs(c.DataDir)
	if err != nil {
		return c, fmt.Errorf("resolve data directory: %w", err)
	}
	return c, nil
}
