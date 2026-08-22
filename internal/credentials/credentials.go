package credentials

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
)

var preferredOrder = []string{"XAI_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY"}

func Path() string {
	return filepath.Join(config.AppSupportDir(), "credentials.env")
}

func Load() (map[string]string, error) {
	out := map[string]string{}
	f, err := os.Open(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k != "" && v != "" {
			out[k] = v
		}
	}
	return out, sc.Err()
}

func Save(envName, value string) error {
	if envName == "" {
		return fmt.Errorf("empty credential name")
	}
	if !validCredName(envName) {
		return fmt.Errorf("invalid credential name %q", envName)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("empty credential")
	}
	cur, err := Load()
	if err != nil {
		return err
	}
	cur[envName] = value
	if err := os.MkdirAll(config.AppSupportDir(), 0o700); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# ABox credentials. Mode 0600. Do not commit.\n")
	written := map[string]struct{}{}
	write := func(name string) {
		if v := cur[name]; v != "" {
			b.WriteString(name)
			b.WriteByte('=')
			b.WriteString(v)
			b.WriteByte('\n')
			written[name] = struct{}{}
		}
	}
	for _, name := range preferredOrder {
		write(name)
	}
	var rest []string
	for name := range cur {
		if _, ok := written[name]; ok {
			continue
		}
		if cur[name] == "" {
			continue
		}
		rest = append(rest, name)
	}
	sort.Strings(rest)
	for _, name := range rest {
		write(name)
	}
	tmp := Path() + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, Path()); err != nil {
		return err
	}
	return os.Chmod(Path(), 0o600)
}

func ApplyToEnv() error {
	cur, err := Load()
	if err != nil {
		return err
	}
	for name, v := range cur {
		if os.Getenv(name) != "" {
			continue
		}
		if v != "" {
			_ = os.Setenv(name, v)
		}
	}
	return nil
}

func validCredName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'A' && r <= 'Z', r == '_':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func SetEnv(envName, value string) {
	_ = os.Setenv(envName, strings.TrimSpace(value))
}

func Present(envName string) bool {
	return strings.TrimSpace(os.Getenv(envName)) != ""
}
