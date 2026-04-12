package state

import (
	"log"
	"os"
	"strconv"
	"sync"
	"time"
)

// State 核心服务状态 (仅核心服务使用)
// 包含 sessions 等不需要跨进程共享的内存状态
type State struct {
	mu         sync.Mutex
	ReceiveDir string
	// 端口配置
	CorePort  int // LocalSend 端口
	AdminPort int // 管理面板端口

	Logs       []LogEntry
	MaxLogs    int
	// Sessions 记录 Session 信息，用于在 Upload 阶段获取文件名 (不持久化)
	Sessions map[string]map[string]string
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
		Sessions:    make(map[string]map[string]string),
	}

	// 2. 尝试加载配置文件 (覆盖默认值)
	if s.loadFromFile() {
		log.Println("✅ Configuration loaded from file.")
	} else {
		log.Println("ℹ️ No config file found, using defaults.")
	}

	// 3. 环境变量覆盖 (最高优先级)
	applyEnvOverrides(s)

	log.Printf("   📁 Receive Directory: %s", s.ReceiveDir)
	log.Printf("   🌐 LocalSend Port: %d (HTTPS)", s.CorePort)
	log.Printf("   🛡️ Admin Console: http://127.0.0.1:%d", s.AdminPort)

	// 确保接收目录存在
	os.MkdirAll(s.ReceiveDir, 0755)

	// 启动定时保存
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			s.saveToFile()
		}
	}()

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
func (s *State) RegisterSession(sessionID string, fileMap map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Sessions[sessionID] = fileMap
}

// ResolveFileName 根据 SessionID 和 FileID 解析文件名
func (s *State) ResolveFileName(sessionID, fileID, fallbackName string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sessionMap, ok := s.Sessions[sessionID]; ok {
		if name, ok := sessionMap[fileID]; ok {
			return name
		}
	}
	return fallbackName
}

// AddLog 线程安全地添加日志，并自动清理旧日志
func (s *State) AddLog(filename string, size int64, sender string, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := LogEntry{
		Time:     time.Now().Format("15:04:05"),
		Filename: filename,
		Size:     size,
		Sender:   sender,
		Status:   status,
	}

	s.Logs = append(s.Logs, entry)
	// 环形缓冲逻辑：超出限制删掉最老的
	if len(s.Logs) > s.MaxLogs {
		s.Logs = s.Logs[1:]
	}
}

// GetLogs 线程安全地获取日志（倒序）
func (s *State) GetLogs() []LogEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 复制并倒序
	res := make([]LogEntry, len(s.Logs))
	for i, v := range s.Logs {
		res[len(s.Logs)-1-i] = v
	}
	return res
}

// ClearLogs 清空日志
func (s *State) ClearLogs() {
	s.mu.Lock()
	s.Logs = nil
	s.mu.Unlock()
	// 清空操作也需要保存，否则重启日志又回来了
	// 注意: Save() 必须在锁外面调用，避免死锁
	s.Save()
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
