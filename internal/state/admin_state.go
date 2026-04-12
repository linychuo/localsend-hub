package state

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"localsend-hub/internal/db"
)

// AdminState 管理服务状态 (仅管理服务使用)
// 从配置文件读取状态，独立于核心服务
type AdminState struct {
	mu         sync.Mutex
	ReceiveDir string
	Alias      string
	DeviceModel string
	DeviceType string
	CorePort   int
	AdminPort  int
	MaxLogs    int
	// LogDB SQLite 数据库实例 (跨进程共享)
	LogDB *db.LogDB
	// configPath 配置文件路径
	configPath string
	// watchInterval 配置文件监控间隔
	watchInterval time.Duration
	// lastModTime 最后修改时间
	lastModTime time.Time
}

// NewAdminState 创建管理服务状态实例
// 会从配置文件读取并监控配置变化
func NewAdminState() *AdminState {
	s := &AdminState{
		ReceiveDir:    "./received",
		CorePort:      53317,
		AdminPort:     53318,
		MaxLogs:       1000,
		Alias:         "LocalSend Hub",
		DeviceModel:   "LocalSend Hub Server",
		DeviceType:    "server",
		configPath:    GetConfigPath(),
		watchInterval: 2 * time.Second, // 每2秒检查一次配置变化
	}

	// 加载初始配置
	s.loadFromConfigFile()

	// 初始化 SQLite 数据库
	logDB, err := db.NewLogDB(s.MaxLogs)
	if err != nil {
		log.Printf("❌ Admin: Failed to initialize log database: %v", err)
	} else {
		s.LogDB = logDB
		log.Println("✅ Admin: Log database initialized.")
	}

	// 确保接收目录存在
	os.MkdirAll(s.ReceiveDir, 0755)

	// 启动配置文件监控
	go s.watchConfigFile()

	return s
}

// watchConfigFile 监控配置文件变化
func (s *AdminState) watchConfigFile() {
	ticker := time.NewTicker(s.watchInterval)
	defer ticker.Stop()

	for range ticker.C {
		s.reloadIfConfigChanged()
	}
}

// reloadIfConfigChanged 检查配置文件是否被核心服务修改并重新加载
func (s *AdminState) reloadIfConfigChanged() bool {
	info, err := os.Stat(s.configPath)
	if err != nil {
		return false
	}

	if info.ModTime().After(s.lastModTime) {
		s.lastModTime = info.ModTime()
		s.loadFromConfigFile()
		return true
	}
	return false
}

// loadFromConfigFile 从配置文件读取状态
func (s *AdminState) loadFromConfigFile() {
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		// 配置文件不存在或读取失败，使用默认值
		log.Printf("⚠️ Admin: Config file not found or unreadable: %v", err)
		return
	}

	var config ConfigData
	if err := json.Unmarshal(data, &config); err != nil {
		log.Printf("❌ Admin: Failed to parse config file: %v", err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 应用配置文件中的值
	if config.ReceiveDir != "" {
		s.ReceiveDir = config.ReceiveDir
	}
	if config.CorePort != 0 {
		s.CorePort = config.CorePort
	}
	if config.AdminPort != 0 {
		s.AdminPort = config.AdminPort
	}
	if config.MaxLogs > 0 {
		s.MaxLogs = config.MaxLogs
	}
	if config.Alias != "" {
		s.Alias = config.Alias
	}
	if config.DeviceModel != "" {
		s.DeviceModel = config.DeviceModel
	}
	if config.DeviceType != "" {
		s.DeviceType = config.DeviceType
	}
}

// saveToFile 将当前状态写入配置文件
func (s *AdminState) saveToFile() {
	configDir := filepath.Dir(s.configPath)

	// Ensure config directory exists
	if err := os.MkdirAll(configDir, 0755); err != nil {
		log.Printf("❌ Admin: Failed to create config directory: %v", err)
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
	}
	s.mu.Unlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		log.Printf("❌ Admin: Failed to marshal config: %v", err)
		return
	}

	if err := os.WriteFile(s.configPath, data, 0644); err != nil {
		log.Printf("❌ Admin: Failed to write config file: %v", err)
		return
	}

	// 更新最后修改时间
	if info, err := os.Stat(s.configPath); err == nil {
		s.lastModTime = info.ModTime()
	}
}

// GetDeviceIdentity 获取完整的设备身份信息
func (s *AdminState) GetDeviceIdentity() (string, string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Alias, s.DeviceModel, s.DeviceType
}

// SetDeviceIdentity 设置设备身份信息
func (s *AdminState) SetDeviceIdentity(alias, model, deviceType string) {
	s.mu.Lock()
	if alias != "" {
		s.Alias = alias
	}
	if model != "" {
		s.DeviceModel = model
	}
	if deviceType != "" {
		s.DeviceType = deviceType
	}
	s.mu.Unlock()

	// 修改了关键配置，立即保存
	s.saveToFile()
}

// GetLogs 线程安全地获取日志（倒序，最新的在前）
func (s *AdminState) GetLogs() []LogEntry {
	if s.LogDB == nil {
		return []LogEntry{}
	}
	logs, err := s.LogDB.GetLogs()
	if err != nil {
		log.Printf("❌ Admin: Failed to get logs: %v", err)
		return []LogEntry{}
	}
	return logs
}

// ClearLogs 清空日志 (通过 SQLite)
func (s *AdminState) ClearLogs() {
	if s.LogDB == nil {
		return
	}
	if err := s.LogDB.ClearLogs(); err != nil {
		log.Printf("❌ Admin: Failed to clear logs: %v", err)
	}
}

// SetReceiveDir 修改接收目录
func (s *AdminState) SetReceiveDir(dir string) {
	// Prevent empty directory path
	if dir == "" {
		log.Println("⚠️ Admin: Attempted to set empty receive directory, ignoring")
		return
	}

	s.mu.Lock()
	s.ReceiveDir = dir
	s.mu.Unlock()

	// 修改了关键配置，立即保存
	s.saveToFile()
}

// GetReceiveDir 获取接收目录
func (s *AdminState) GetReceiveDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ReceiveDir
}

// GetCorePort 获取核心服务端口
func (s *AdminState) GetCorePort() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.CorePort
}

// GetAdminPort 获取管理服务端口
func (s *AdminState) GetAdminPort() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.AdminPort
}

// GetMaxLogs 获取最大日志数量
func (s *AdminState) GetMaxLogs() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.MaxLogs
}
