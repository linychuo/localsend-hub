package state

import (
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"localsend-hub/internal/db"
)

// FileMeta 存储文件的元信息
type FileMeta struct {
	FileName string
	Modified *time.Time
}

// State 核心服务状态 (仅核心服务使用)
// 包含 sessions 等不需要跨进程共享的内存状态
type State struct {
	mu         sync.Mutex
	ReceiveDir string
	// 端口配置
	CorePort  int // LocalSend 端口
	AdminPort int // 管理面板端口

	MaxLogs int
	// LogDB SQLite 数据库实例 (跨进程共享)
	LogDB *db.LogDB
	// Sessions 记录 Session 的文件映射 (不持久化)
	Sessions map[string]map[string]*FileMeta
	// CancelSessions 记录被取消的 Session (用于中断上传)
	CancelSessions map[string]bool
	// 设备信息配置
	Alias       string
	DeviceModel string
	DeviceType  string
}

// New 创建核心服务状态实例
//
// 配置加载顺序:
// 1. 代码默认值 (fallback)
// 2. 配置文件覆盖 (如果存在)
// 3. 环境变量覆盖 (如果设置，最高优先级)
//
// Admin UI 修改的设置会保存到配置文件。
// 如果对应的环境变量在重启时被设置，环境变量的值会覆盖配置文件。
func New() *State {
	// 1. 代码默认值
	s := &State{
		ReceiveDir:  "./received",
		CorePort:    53317,
		AdminPort:   53318,
		MaxLogs:     1000,
		Alias:       "LocalSend Hub",
		DeviceModel: "LocalSend Hub Server",
		DeviceType:  "server",
		Sessions:     make(map[string]map[string]*FileMeta),
		CancelSessions: make(map[string]bool),
	}

	// 2. 尝试加载配置文件 (覆盖默认值)
	if s.loadFromFile() {
		log.Println("✅ Configuration loaded from file.")
	} else {
		log.Println("ℹ️ No config file found, using defaults.")
	}

	// 3. 环境变量覆盖 (最高优先级)
	applyEnvOverrides(s)

	// 初始化 SQLite 数据库
	logDB, err := db.NewLogDB(s.MaxLogs)
	if err != nil {
		log.Printf("❌ Failed to initialize log database: %v", err)
	} else {
		s.LogDB = logDB
		log.Println("✅ Log database initialized.")
	}

	log.Printf("   📁 Receive Directory: %s", s.ReceiveDir)
	log.Printf("   🌐 LocalSend Port: %d (HTTPS)", s.CorePort)
	log.Printf("   🛡️ Admin Console: http://127.0.0.1:%d", s.AdminPort)

	// 确保接收目录存在
	os.MkdirAll(s.ReceiveDir, 0755)

	return s
}

// applyEnvOverrides 应用环境变量覆盖配置
func applyEnvOverrides(s *State) {
	if v := os.Getenv("LOCALSEND_RECEIVE_DIR"); v != "" {
		s.ReceiveDir = v
		log.Printf("📁 Receive dir overridden by env var: %s", v)
	}
	if v := os.Getenv("LOCALSEND_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			s.CorePort = port
			log.Printf("🌐 Core port overridden by env var: %d", port)
		}
	}
	if v := os.Getenv("LOCALSEND_ADMIN_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			s.AdminPort = port
			log.Printf("🛡️ Admin port overridden by env var: %d", port)
		}
	}
	if v := os.Getenv("LOCALSEND_MAX_LOGS"); v != "" {
		if maxLogs, err := strconv.Atoi(v); err == nil {
			s.MaxLogs = maxLogs
			log.Printf("📊 Max logs overridden by env var: %d", maxLogs)
		}
	}
	if v := os.Getenv("LOCALSEND_DEVICE_NAME"); v != "" {
		s.Alias = v
		s.DeviceModel = v + " Server"
		log.Printf("📝 Device name overridden by env var: %s", v)
	}
	if v := os.Getenv("LOCALSEND_DEVICE_TYPE"); v != "" {
		s.DeviceType = v
		log.Printf("🖥️ Device type overridden by env var: %s", v)
	}
}

// Save 触发手动保存 (供外部 API 调用)
func (s *State) Save() {
	s.saveToFile()
}

// GetDeviceIdentity 获取完整的设备身份信息
func (s *State) GetDeviceIdentity() (string, string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Alias, s.DeviceModel, s.DeviceType
}

// SetDeviceIdentity 设置设备身份信息
func (s *State) SetDeviceIdentity(alias, model, deviceType string) {
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
	s.Save()
}

// RegisterSession 记录 Session 的文件映射
func (s *State) RegisterSession(sessionID string, fileMap map[string]*FileMeta) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Sessions[sessionID] = fileMap
}

// ResolveFileMeta 根据 SessionID 和 FileID 获取文件元信息
func (s *State) ResolveFileMeta(sessionID, fileID string) *FileMeta {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sessionMap, ok := s.Sessions[sessionID]; ok {
		if meta, ok := sessionMap[fileID]; ok {
			return meta
		}
	}
	return &FileMeta{FileName: fileID}
}

// ResolveFileName 根据 SessionID 和 FileID 解析文件名
func (s *State) ResolveFileName(sessionID, fileID, fallbackName string) string {
	meta := s.ResolveFileMeta(sessionID, fileID)
	if meta.FileName != "" {
		return meta.FileName
	}
	return fallbackName
}

// CancelSession 标记 Session 为已取消，并清理映射
func (s *State) CancelSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CancelSessions[sessionID] = true
	delete(s.Sessions, sessionID)
}

// IsSessionCancelled 检查 Session 是否已被取消
func (s *State) IsSessionCancelled(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.CancelSessions[sessionID]
}

// AddLog 线程安全地添加日志，并自动清理旧日志
func (s *State) AddLog(filename string, size int64, sender string, status string) {
	if s.LogDB == nil {
		log.Println("⚠️ Log database not initialized, skipping log entry")
		return
	}
	if err := s.LogDB.AddLog(filename, size, sender, status); err != nil {
		log.Printf("❌ Failed to add log entry: %v", err)
	}
}

// GetLogs 线程安全地获取日志（倒序，最新的在前）
func (s *State) GetLogs() []LogEntry {
	if s.LogDB == nil {
		return []LogEntry{}
	}
	logs, err := s.LogDB.GetLogs()
	if err != nil {
		log.Printf("❌ Failed to get logs: %v", err)
		return []LogEntry{}
	}
	return logs
}

// ClearLogs 清空日志
func (s *State) ClearLogs() {
	if s.LogDB == nil {
		return
	}
	if err := s.LogDB.ClearLogs(); err != nil {
		log.Printf("❌ Failed to clear logs: %v", err)
	}
}

// SetReceiveDir 修改接收目录
func (s *State) SetReceiveDir(dir string) {
	// Prevent empty directory path
	if dir == "" {
		log.Println("⚠️ Attempted to set empty receive directory, ignoring")
		return
	}

	s.mu.Lock()
	s.ReceiveDir = dir
	s.mu.Unlock()

	// 修改了关键配置，立即保存
	s.Save()
}

// GetReceiveDir 获取接收目录
func (s *State) GetReceiveDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ReceiveDir
}
