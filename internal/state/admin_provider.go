package state

// AdminStateProvider 管理服务需要的状态接口
// State (核心服务) 和 AdminState (管理服务) 都实现此接口
type AdminStateProvider interface {
	GetDeviceIdentity() (string, string, string)
	SetDeviceIdentity(alias, model, deviceType string)
	GetLogs() []LogEntry
	ClearLogs()
	GetReceiveDir() string
	SetReceiveDir(dir string)
}
