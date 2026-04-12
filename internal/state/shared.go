package state

import "localsend-hub/internal/db"

// LogEntry 日志条目 (别名，指向 db.LogEntry)
type LogEntry = db.LogEntry

// ConfigData 配置文件的 JSON 结构 (两个进程共享)
// 注意: Logs 已移除，现在存储在 SQLite 数据库中
type ConfigData struct {
	ReceiveDir  string `json:"receiveDir"`
	CorePort    int    `json:"corePort"`
	AdminPort   int    `json:"adminPort"`
	MaxLogs     int    `json:"maxLogs"`
	Alias       string `json:"alias"`
	DeviceModel string `json:"deviceModel"`
	DeviceType  string `json:"deviceType"`
}
