package db

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"
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

	// WAL 模式支持并发读写
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		log.Printf("⚠️ Failed to set WAL mode: %v", err)
	}

	// 创建表
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS transfer_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			time TEXT NOT NULL,
			filename TEXT NOT NULL,
			size INTEGER NOT NULL,
			sender TEXT NOT NULL,
			status TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_logs_time ON transfer_logs(time);
	`); err != nil {
		return nil, err
	}

	l := &LogDB{
		db:  db,
		max: maxLogs,
	}

	return l, nil
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
