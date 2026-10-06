// Package credential resolves vault entries through the hyprvault / gopass
// "credential" CLI; the vault password never leaves memory.
package credential

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Credential struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	URL      string `json:"url,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password"`
}

// Get resolves ref (entry ID or title) with the vault master password.
func Get(ref, master string) (Credential, error) {
	var c Credential
	err := run([]string{"credential", "get", "--ref", ref, "--password-stdin"}, master, &c)
	return c, err
}

// List returns every vault entry (without passwords) for the picker.
func List(master string) ([]Credential, error) {
	var l []Credential
	err := run([]string{"credential", "list", "--password-stdin"}, master, &l)
	return l, err
}

func run(args []string, master string, out any) error {
	exe, err := findGopass()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdin = strings.NewReader(master + "\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("vault: %s", msg)
	}
	if err := json.Unmarshal(stdout.Bytes(), out); err != nil {
		return fmt.Errorf("decode vault response: %w", err)
	}
	return nil
}

func findGopass() (string, error) {
	for _, name := range []string{"hyprvault", "gopass"} {
		if exe, err := exec.LookPath(name); err == nil {
			return exe, nil
		}
		if cur, err := os.Executable(); err == nil {
			cand := filepath.Join(filepath.Dir(cur), name)
			if info, err := os.Stat(cand); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				return cand, nil
			}
		}
	}
	return "", fmt.Errorf("hyprvault or gopass was not found in PATH or beside hyprdb")
}
