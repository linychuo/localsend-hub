package state

// Shared types for cross-process communication via config file

// LogEntry 传输日志条目
type LogEntry struct {
	Time     string `json:"time"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	Sender   string `json:"sender"`
	Status   string `json:"status"`
}

// ConfigData 配置文件的 JSON 结构 (两个进程共享)
type ConfigData struct {
	ReceiveDir  string     `json:"receiveDir"`
	CorePort    int        `json:"corePort"`
	AdminPort   int        `json:"adminPort"`
	MaxLogs     int        `json:"maxLogs"`
	Alias       string     `json:"alias"`
	DeviceModel string     `json:"deviceModel"`
	DeviceType  string     `json:"deviceType"`
	Logs        []LogEntry `json:"logs,omitempty"`
}
