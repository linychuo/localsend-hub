package state

import (
	"context"
	"io"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"localsend-hub/internal/db"
)

// sessionTTL 是 prepare-upload 创建的 session 的空闲超时时间
// 只要 session 还在持续活动 (有文件在传或刚传完一个) 就会被刷新，不会过期；
// 只有真正闲置超过该时长、且无任何进行中上传的僵尸 session 才会被清理，避免内存泄漏
const sessionTTL = 10 * time.Minute

// FileMeta 存储文件的元信息
type FileMeta struct {
	FileName        string
	Size            *int64
	FileType        string
	Sha256          *string
	Modified        *time.Time
	SenderFingerprint string // 发送设备的 TLS 指纹
}

// CancellableReader 是一个可中断的 io.Reader 包装器
// 每次 Read 调用前检查 context 是否已取消
type CancellableReader struct {
	reader io.Reader
	ctx    context.Context
}

// NewCancellableReader 创建可中断的 reader
func NewCancellableReader(ctx context.Context, reader io.Reader) *CancellableReader {
	return &CancellableReader{reader: reader, ctx: ctx}
}

func (r *CancellableReader) Read(p []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(p)
	}
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
	// sessionTokens 存储 prepare-upload 返回的 file tokens (用于 upload 验证)
	// 结构: sessionID -> fileID -> token
	sessionTokens map[string]map[string]string
	// sessionLastActive 记录 session 最近一次活动时间 (创建/文件上传开始或结束)
	// 用于按空闲超时清理僵尸 session：只要还在持续收文件就会被刷新，不会误删长传输
	sessionLastActive map[string]time.Time
	// uploadCancelFuncs 正在进行的上传的 cancel 函数 (用于立即中断)
	// 结构: sessionID -> fileID -> cancel
	uploadCancelFuncs map[string]map[string]context.CancelFunc
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
		Sessions:           make(map[string]map[string]*FileMeta),
		sessionTokens:      make(map[string]map[string]string),
		sessionLastActive:  make(map[string]time.Time),
		uploadCancelFuncs:  make(map[string]map[string]context.CancelFunc),
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

	// 启动僵尸 session 清理 (仅生产实例，测试实例不启动 goroutine)
	go s.sweepExpiredSessions()

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

// NewForTesting 构造一个仅用于测试的 State 实例
// 不读取配置文件、不应用环境变量、不初始化 SQLite、不启动 goroutine
// 接收目录由调用方指定 (通常是 t.TempDir())
func NewForTesting(receiveDir string) *State {
	return &State{
		ReceiveDir:        receiveDir,
		CorePort:          53317,
		AdminPort:         53318,
		MaxLogs:           1000,
		Alias:             "TestHub",
		DeviceModel:       "TestHub Server",
		DeviceType:        "server",
		Sessions:           make(map[string]map[string]*FileMeta),
		sessionTokens:      make(map[string]map[string]string),
		sessionLastActive:  make(map[string]time.Time),
		uploadCancelFuncs:  make(map[string]map[string]context.CancelFunc),
	}
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
func (s *State) RegisterSession(sessionID string, fileMap map[string]*FileMeta, tokens map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Sessions[sessionID] = fileMap
	// 存储 tokens 用于后续 upload 验证
	s.sessionTokens[sessionID] = tokens
	// 记录最近活跃时间，用于空闲超时清理
	s.sessionLastActive[sessionID] = time.Now()
}

// ValidateToken 验证 file token 是否匹配
func (s *State) ValidateToken(sessionID, fileID, token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if tokens, ok := s.sessionTokens[sessionID]; ok {
		if expected, ok := tokens[fileID]; ok {
			return expected == token
		}
	}
	return false
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

// CancelSession 标记 Session 为已取消，并清理映射
// 会触发该 session 下所有正在进行的上传的 cancel 函数
func (s *State) CancelSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 触发该 session 下所有正在进行的上传的 cancel
	if cancels, ok := s.uploadCancelFuncs[sessionID]; ok {
		for _, cancel := range cancels {
			cancel()
		}
	}
	delete(s.Sessions, sessionID)
	delete(s.uploadCancelFuncs, sessionID)
	delete(s.sessionTokens, sessionID)
	delete(s.sessionLastActive, sessionID)
}

// RegisterUploadCancel 注册某个文件上传的 cancel 函数，用于中途取消传输
// 同一 session 下多个文件并发上传时，按 fileID 分别存储，互不覆盖
// 同时刷新 session 的最近活跃时间，保证长文件传输期间不会被空闲超时清理
func (s *State) RegisterUploadCancel(sessionID, fileID string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.uploadCancelFuncs[sessionID] == nil {
		s.uploadCancelFuncs[sessionID] = make(map[string]context.CancelFunc)
	}
	s.uploadCancelFuncs[sessionID][fileID] = cancel
	s.sessionLastActive[sessionID] = time.Now()
}

// CleanupUpload 清理某个文件上传的 cancel 函数和 token（单个文件上传完成时调用）
// 当 session 下所有文件都已清理时，回收整个 session 的映射
func (s *State) CleanupUpload(sessionID, fileID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancels, ok := s.uploadCancelFuncs[sessionID]; ok {
		delete(cancels, fileID)
		if len(cancels) == 0 {
			delete(s.uploadCancelFuncs, sessionID)
		}
	}
	if tokens, ok := s.sessionTokens[sessionID]; ok {
		delete(tokens, fileID)
		if len(tokens) == 0 {
			delete(s.sessionTokens, sessionID)
		}
	}
	if files, ok := s.Sessions[sessionID]; ok {
		delete(files, fileID)
		if len(files) == 0 {
			delete(s.Sessions, sessionID)
			delete(s.sessionLastActive, sessionID)
		} else {
			// session 还有未完成上传，刷新活跃时间，防止被空闲超时清理
			s.sessionLastActive[sessionID] = time.Now()
		}
	}
}

// sweepExpiredSessions 定期清理僵尸 session
// 只清理没有任何进行中上传 (uploadCancelFuncs 为空) 且空闲超过 sessionTTL 的 session，
// 活跃时间由 RegisterSession / RegisterUploadCancel / CleanupUpload 刷新，
// 避免误删正在传输大文件、或多文件批次传输中长时间跨度的活跃 session
func (s *State) sweepExpiredSessions() {
	ticker := time.NewTicker(sessionTTL / 2)
	defer ticker.Stop()

	for range ticker.C {
		s.sweepExpiredSessionsOnce(time.Now(), sessionTTL)
	}
}

// sweepExpiredSessionsOnce 执行一轮僵尸 session 清理 (可注入 now/ttl 用于测试)
// 返回被清理的 session 数量
func (s *State) sweepExpiredSessionsOnce(now time.Time, ttl time.Duration) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	cleaned := 0
	for sid, lastActive := range s.sessionLastActive {
		if len(s.uploadCancelFuncs[sid]) > 0 {
			continue // 有进行中的上传，跳过
		}
		if now.Sub(lastActive) > ttl {
			delete(s.Sessions, sid)
			delete(s.sessionTokens, sid)
			delete(s.uploadCancelFuncs, sid)
			delete(s.sessionLastActive, sid)
			log.Printf("🧹 Cleaned up expired session: %s", sid)
			cleaned++
		}
	}
	return cleaned
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

// CloseDB 关闭日志数据库连接 (优雅退出时调用)
func (s *State) CloseDB() {
	if s.LogDB != nil {
		if err := s.LogDB.Close(); err != nil {
			log.Printf("⚠️ Failed to close log database: %v", err)
		}
	}
}
