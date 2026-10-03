package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// px1 keeps no state inside a workspace. The user's preferences and choices
// are stored with the other per-user files px1 writes (~/.px1/settings.json),
// never in the working tree.

type settings struct {
	Agent           string            `json:"agent,omitempty"`
	Models          map[string]string `json:"models,omitempty"`
	ReviewAutoStart *bool             `json:"review.autoStart,omitempty"`
}

var settingsMu sync.Mutex

// settingsPath mirrors stateFilePath in update.go: honour the XDG location when
// it is set, otherwise fall back to ~/.px1.
func settingsPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "px1", "settings.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".px1", "settings.json")
}

// readSettingsRawMap returns the raw JSON contents unmarshaled into a map.
// It never fails: a missing or corrupt file returns an empty map.
func readSettingsRawMap() map[string]any {
	p := settingsPath()
	if p == "" {
		return map[string]any{}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil || m == nil {
		return map[string]any{}
	}
	return m
}

// readSettings never fails: a missing or corrupt file simply means no choice
// has been made yet, which is the same as a fresh install.
func readSettings() settings {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	return readSettingsLocked()
}

func readSettingsLocked() settings {
	var s settings
	raw := readSettingsRawMap()
	if len(raw) == 0 {
		return s
	}

	if b, err := json.Marshal(raw); err == nil {
		_ = json.Unmarshal(b, &s)
	}

	// Keep the legacy agent and models keys compatible with their namespaced
	// counterparts used by the raw settings editor.
	if s.Agent == "" {
		if h, ok := raw["agent.harness"].(string); ok && h != "" {
			s.Agent = h
		}
	}
	if s.Models == nil || len(s.Models) == 0 {
		if am, ok := raw["agent.models"].(map[string]any); ok {
			s.Models = make(map[string]string, len(am))
			for k, v := range am {
				if vs, ok := v.(string); ok {
					s.Models[k] = vs
				}
			}
		}
	}

	return s
}

// readMergedSettingsMap returns only persisted settings. Defaults belong to
// runtime consumers; the settings editor is intentionally a raw JSON escape
// hatch rather than an IDE preference catalog.
func readMergedSettingsMap() map[string]any {
	settingsMu.Lock()
	defer settingsMu.Unlock()

	raw := readSettingsRawMap()
	res := make(map[string]any, len(raw)+2)
	for k, v := range raw {
		res[k] = v
	}

	if ag, ok := raw["agent"].(string); ok && ag != "" {
		res["agent.harness"] = ag
	} else if ah, ok := raw["agent.harness"].(string); ok && ah != "" {
		res["agent"] = ah
	}

	if m, ok := raw["models"].(map[string]any); ok && len(m) > 0 {
		res["agent.models"] = m
	} else if am, ok := raw["agent.models"].(map[string]any); ok && len(am) > 0 {
		res["models"] = am
	}

	return res
}

// readRawSettingsJSON returns formatted settings.json file content as string.
func readRawSettingsJSON() string {
	settingsMu.Lock()
	defer settingsMu.Unlock()

	p := settingsPath()
	if p == "" {
		return "{}\n"
	}
	data, err := os.ReadFile(p)
	if err != nil || len(data) == 0 {
		return "{\n}\n"
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err == nil {
		if formatted, err := json.MarshalIndent(raw, "", "  "); err == nil {
			return string(formatted) + "\n"
		}
	}
	return string(data)
}

// writeSettings saves the agent and models choices while preserving other settings.
func writeSettings(s settings) error {
	p := settingsPath()
	if p == "" {
		return errors.New("no home directory to save settings in")
	}
	settingsMu.Lock()
	defer settingsMu.Unlock()

	raw := readSettingsRawMap()
	if s.Agent != "" {
		raw["agent"] = s.Agent
		raw["agent.harness"] = s.Agent
	} else {
		delete(raw, "agent")
		delete(raw, "agent.harness")
	}

	if s.Models != nil && len(s.Models) > 0 {
		raw["models"] = s.Models
		raw["agent.models"] = s.Models
	} else if s.Agent == "" {
		delete(raw, "models")
		delete(raw, "agent.models")
	}

	return writeRawMapLocked(p, raw)
}

// updateSettingsMap merges key-value pairs into settings.json without losing existing keys.
func updateSettingsMap(updates map[string]any) error {
	p := settingsPath()
	if p == "" {
		return errors.New("no home directory to save settings in")
	}
	settingsMu.Lock()
	defer settingsMu.Unlock()

	raw := readSettingsRawMap()
	for k, v := range updates {
		if v == nil {
			delete(raw, k)
		} else {
			raw[k] = v
		}

		if k == "agent" {
			if v == nil || v == "" {
				delete(raw, "agent.harness")
			} else {
				raw["agent.harness"] = v
			}
		} else if k == "agent.harness" {
			if v == nil || v == "" {
				delete(raw, "agent")
			} else {
				raw["agent"] = v
			}
		}

		if k == "models" {
			if v == nil {
				delete(raw, "agent.models")
			} else {
				raw["agent.models"] = v
			}
		} else if k == "agent.models" {
			if v == nil {
				delete(raw, "models")
			} else {
				raw["models"] = v
			}
		}
	}

	return writeRawMapLocked(p, raw)
}

// saveRawSettingsJSON parses and validates raw JSON text and writes it formatted.
func saveRawSettingsJSON(rawJSON []byte) error {
	var m map[string]any
	if err := json.Unmarshal(rawJSON, &m); err != nil {
		return err
	}

	p := settingsPath()
	if p == "" {
		return errors.New("no home directory to save settings in")
	}
	settingsMu.Lock()
	defer settingsMu.Unlock()

	if ag, ok := m["agent"].(string); ok && ag != "" {
		m["agent.harness"] = ag
	} else if ah, ok := m["agent.harness"].(string); ok && ah != "" {
		m["agent"] = ah
	}

	return writeRawMapLocked(p, m)
}

func writeRawMapLocked(p string, raw map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o644)
}
