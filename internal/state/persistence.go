package state

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// GetConfigPath returns the path to the config file.
// It checks the LOCALSEND_CONFIG_PATH environment variable first,
// then falls back to common Docker paths, then the current directory.
func GetConfigPath() string {
	if p := os.Getenv("LOCALSEND_CONFIG_PATH"); p != "" {
		return p
	}
	// Docker default path (matches entrypoint.sh)
	if _, err := os.Stat("/app/config"); err == nil {
		return "/app/config/localsend_config.json"
	}
	// Fallback to current directory
	cwd, err := os.Getwd()
	if err != nil {
		return "localsend_config.json"
	}
	return filepath.Join(cwd, "localsend_config.json")
}

// saveToFile 将当前状态写入配置文件
func (s *State) saveToFile() {
	configFile := GetConfigPath()
	configDir := filepath.Dir(configFile)

	// Ensure config directory exists
	if err := os.MkdirAll(configDir, 0755); err != nil {
		log.Printf("❌ Failed to create config directory: %v", err)
		return
	}

	s.mu.Lock()
	snapshot := ConfigData{
		ReceiveDir:  s.ReceiveDir,
		CorePort:    s.CorePort,
		AdminPort:   s.AdminPort,
		MaxLogs:     s.MaxLogs,
		Alias:       s.Alias,
		DeviceModel: s.DeviceModel,
		DeviceType:  s.DeviceType,
		Logs:        s.Logs,
	}
	s.mu.Unlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		log.Printf("❌ Failed to marshal config: %v", err)
		return
	}

	if err := os.WriteFile(configFile, data, 0644); err != nil {
		log.Printf("❌ Failed to write config file: %v", err)
	}
}

// loadFromFile 从配置文件读取状态
// 如果返回 false，说明文件不存在或读取失败
func (s *State) loadFromFile() bool {
	configFile := GetConfigPath()
	data, err := os.ReadFile(configFile)
	if err != nil {
		return false
	}

	var snapshot ConfigData
	if err := json.Unmarshal(data, &snapshot); err != nil {
		log.Printf("❌ Failed to parse config file: %v", err)
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 应用配置文件中的值 (只覆盖非空值)
	if snapshot.ReceiveDir != "" {
		s.ReceiveDir = snapshot.ReceiveDir
	}
	if snapshot.CorePort != 0 {
		s.CorePort = snapshot.CorePort
	}
	if snapshot.AdminPort != 0 {
		s.AdminPort = snapshot.AdminPort
	}
	if snapshot.MaxLogs > 0 {
		s.MaxLogs = snapshot.MaxLogs
	}
	if snapshot.Alias != "" {
		s.Alias = snapshot.Alias
	}
	if snapshot.DeviceModel != "" {
		s.DeviceModel = snapshot.DeviceModel
	}
	if snapshot.DeviceType != "" {
		s.DeviceType = snapshot.DeviceType
	}
	if snapshot.Logs != nil {
		s.Logs = snapshot.Logs
	}

	return true
}
