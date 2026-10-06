// Package db holds saved connections and the MySQL/MariaDB/PostgreSQL access layer.
package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Kind string

const (
	MySQL    Kind = "mysql"
	MariaDB  Kind = "mariadb"
	Postgres Kind = "postgres"
)

var Kinds = []Kind{MySQL, MariaDB, Postgres}

func (k Kind) DefaultPort() int {
	if k == Postgres {
		return 5432
	}
	return 3306
}

func (k Kind) Label() string {
	switch k {
	case MariaDB:
		return "MariaDB"
	case Postgres:
		return "PostgreSQL"
	}
	return "MySQL"
}

// Config is one saved connection. Passwords are never stored: CredentialRef
// names a hyprvault / gopass entry, or the password is asked at connect time.
type Config struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Kind          Kind   `json:"kind"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	User          string `json:"user"`
	Database      string `json:"database,omitempty"`
	CredentialRef string `json:"credential_ref,omitempty"`
}

func (c Config) Addr() string { return c.Host + ":" + strconv.Itoa(c.Port) }

func (c Config) Validate() error {
	switch {
	case strings.TrimSpace(c.Name) == "":
		return errors.New("name is required")
	case strings.TrimSpace(c.Host) == "":
		return errors.New("host is required")
	case c.Port <= 0 || c.Port > 65535:
		return errors.New("port must be 1-65535")
	case strings.TrimSpace(c.User) == "":
		return errors.New("user is required")
	case c.Kind == Postgres && strings.TrimSpace(c.Database) == "":
		return errors.New("PostgreSQL needs a database name")
	}
	return nil
}

func ConfigPath() string {
	dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "hyprdb", "connections.json")
}

func LoadConfigs() ([]Config, error) {
	data, err := os.ReadFile(ConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfgs []Config
	if err := json.Unmarshal(data, &cfgs); err != nil {
		return nil, fmt.Errorf("%s: %w", ConfigPath(), err)
	}
	return cfgs, nil
}

func SaveConfigs(cfgs []Config) error {
	for i := range cfgs {
		if cfgs[i].ID == "" {
			cfgs[i].ID = strconv.FormatInt(time.Now().UnixNano(), 36)
		}
	}
	data, err := json.MarshalIndent(cfgs, "", "  ")
	if err != nil {
		return err
	}
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
