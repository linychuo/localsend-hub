package db

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

// LogEntry 日志条目
type LogEntry struct {
	Time     string `json:"time"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	Sender   string `json:"sender"`
	Status   string `json:"status"`
}

// LogDB 日志数据库操作封装
type LogDB struct {
	db  *sql.DB
	mu  sync.Mutex
	max int
}

// GetDBPath 返回数据库文件路径
func GetDBPath() string {
	if p := os.Getenv("LOCALSEND_DB_PATH"); p != "" {
		return p
	}
	// Docker default path
	if _, err := os.Stat("/app/data"); err == nil {
		return "/app/data/localsend_logs.db"
	}
	// Fallback to current directory
	cwd, err := os.Getwd()
	if err != nil {
		return "localsend_logs.db"
	}
	return filepath.Join(cwd, "localsend_logs.db")
}

// dbExecWithRetry 带重试的数据库执行，用于解决多进程并发打开时的 SQLITE_BUSY 问题
func dbExecWithRetry(db *sql.DB, query string, maxRetries int) error {
	var err error
	for i := 0; i < maxRetries; i++ {
		_, err = db.Exec(query)
		if err == nil {
			return nil
		}
		// Check if error is SQLITE_BUSY (database is locked)
		if strings.Contains(err.Error(), "SQLITE_BUSY") || strings.Contains(err.Error(), "database is locked") {
			delay := time.Duration(i+1) * 200 * time.Millisecond
			log.Printf("⚠️ Database locked, retrying in %v... (%d/%d)", delay, i+1, maxRetries)
			time.Sleep(delay)
			continue
		}
		// Non-retryable error
		return err
	}
	return err
}

// NewLogDB 创建并初始化日志数据库
func NewLogDB(maxLogs int) (*LogDB, error) {
	dbPath := GetDBPath()

	// 确保数据库目录存在
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// WAL 模式支持并发读写 (带重试以解决多进程并发打开冲突)
	if err := dbExecWithRetry(db, "PRAGMA journal_mode=WAL", 5); err != nil {
		log.Printf("⚠️ Failed to set WAL mode: %v", err)
	}

	// Set busy timeout so that all subsequent queries will retry instead of failing immediately
	if err := dbExecWithRetry(db, "PRAGMA busy_timeout=5000", 3); err != nil {
		log.Printf("⚠️ Failed to set busy_timeout: %v", err)
	}

	// 创建表 (带重试)
	if err := dbExecWithRetry(db, `
		CREATE TABLE IF NOT EXISTS transfer_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			time TEXT NOT NULL,
			filename TEXT NOT NULL,
			size INTEGER NOT NULL,
			sender TEXT NOT NULL,
			status TEXT NOT NULL
		)
	`, 5); err != nil {
		db.Close()
		return nil, err
	}

	if err := dbExecWithRetry(db, `
		CREATE INDEX IF NOT EXISTS idx_logs_time ON transfer_logs(time)
	`, 5); err != nil {
		// Index creation failure is non-fatal
		log.Printf("⚠️ Failed to create index: %v", err)
	}

	l := &LogDB{
		db:  db,
		max: maxLogs,
	}

	return l, nil
}

// OpenLogDB 以只读方式打开数据库连接（用于非写入进程，如 Admin 服务）
// 不执行 WAL 设置、表创建等初始化操作，避免多进程锁冲突
func OpenLogDB() (*LogDB, error) {
	dbPath := GetDBPath()

	if _, err := os.Stat(dbPath); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// Set busy timeout for read queries
	db.Exec("PRAGMA busy_timeout=5000")
	db.Exec("PRAGMA journal_mode=WAL")

	return &LogDB{db: db, max: 1000}, nil
}

// AddLog 添加一条日志记录
func (l *LogDB) AddLog(filename string, size int64, sender string, status string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := LogEntry{
		Time:     time.Now().Format("15:04:05"),
		Filename: filename,
		Size:     size,
		Sender:   sender,
		Status:   status,
	}

	_, err := l.db.Exec(
		"INSERT INTO transfer_logs (time, filename, size, sender, status) VALUES (?, ?, ?, ?, ?)",
		entry.Time, entry.Filename, entry.Size, entry.Sender, entry.Status,
	)
	if err != nil {
		return err
	}

	// 清理超出限制的旧日志
	if err := l.trimOldLogs(); err != nil {
		log.Printf("⚠️ Failed to trim old logs: %v", err)
	}

	return nil
}

// GetLogs 获取所有日志（倒序，最新的在前）
func (l *LogDB) GetLogs() ([]LogEntry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	rows, err := l.db.Query("SELECT time, filename, size, sender, status FROM transfer_logs ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []LogEntry
	for rows.Next() {
		var entry LogEntry
		if err := rows.Scan(&entry.Time, &entry.Filename, &entry.Size, &entry.Sender, &entry.Status); err != nil {
			return nil, err
		}
		logs = append(logs, entry)
	}

	if logs == nil {
		logs = []LogEntry{}
	}

	return logs, rows.Err()
}

// ClearLogs 清空所有日志
func (l *LogDB) ClearLogs() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	_, err := l.db.Exec("DELETE FROM transfer_logs")
	return err
}

// trimOldLogs 删除超出最大限制的旧日志
func (l *LogDB) trimOldLogs() error {
	if l.max <= 0 {
		return nil
	}

	_, err := l.db.Exec(`
		DELETE FROM transfer_logs
		WHERE id NOT IN (
			SELECT id FROM transfer_logs ORDER BY id DESC LIMIT ?
		)
	`, l.max)

	return err
}

// Close 关闭数据库连接
func (l *LogDB) Close() error {
	return l.db.Close()
}
