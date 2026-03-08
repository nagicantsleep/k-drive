package mount

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const defaultConfigFileName = "rclone.conf"

type ConfigManager struct {
	mu         sync.Mutex
	configPath string
}

func NewConfigManager() *ConfigManager {
	return &ConfigManager{configPath: defaultConfigPath()}
}

func NewConfigManagerAt(configPath string) *ConfigManager {
	return &ConfigManager{configPath: configPath}
}

func (m *ConfigManager) ConfigPath() string {
	return m.configPath
}

func (m *ConfigManager) WriteRemote(remoteName string, remoteType string, options map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sections, err := m.readSections()
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// Build section: type is always set from remoteType, never overridable by options.
	section := make(map[string]string, len(options)+1)
	for k, v := range options {
		if k == "type" {
			continue
		}
		section[k] = v
	}
	section["type"] = remoteType

	if sections == nil {
		sections = make(map[string]map[string]string)
	}
	sections[remoteName] = section

	return m.writeSectionsAtomic(sections)
}

func (m *ConfigManager) DeleteRemote(remoteName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sections, err := m.readSections()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	delete(sections, remoteName)
	return m.writeSectionsAtomic(sections)
}

func (m *ConfigManager) readSections() (map[string]map[string]string, error) {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return nil, err
	}

	sections := make(map[string]map[string]string)
	var currentSection string

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = line[1 : len(line)-1]
			sections[currentSection] = make(map[string]string)
			continue
		}
		if currentSection != "" {
			idx := strings.IndexByte(line, '=')
			if idx > 0 {
				key := strings.TrimSpace(line[:idx])
				value := strings.TrimSpace(line[idx+1:])
				sections[currentSection][key] = value
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan rclone config: %w", err)
	}

	return sections, nil
}

func (m *ConfigManager) writeSectionsAtomic(sections map[string]map[string]string) error {
	dir := filepath.Dir(m.configPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	var sb strings.Builder

	names := make([]string, 0, len(sections))
	for name := range sections {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		kv := sections[name]
		fmt.Fprintf(&sb, "[%s]\n", name)

		keys := make([]string, 0, len(kv))
		for k := range kv {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, key := range keys {
			fmt.Fprintf(&sb, "%s = %s\n", key, kv[key])
		}
		sb.WriteString("\n")
	}

	// Atomic write: write to temp file, then rename over destination.
	tmpPath := m.configPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(sb.String()), 0o600); err != nil {
		return fmt.Errorf("write rclone config tmp: %w", err)
	}

	if err := os.Rename(tmpPath, m.configPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace rclone config: %w", err)
	}

	return nil
}

func defaultConfigPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return defaultConfigFileName
	}

	return filepath.Join(configDir, "KDrive", defaultConfigFileName)
}
