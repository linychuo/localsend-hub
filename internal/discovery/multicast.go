package discovery

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"
)

// Announcer 负责发送 UDP 多播公告
type Announcer struct {
	port   int
	getInfo func() map[string]interface{}
}

// NewAnnouncer 创建多播广播器
func NewAnnouncer(port int, getInfo func() map[string]interface{}) *Announcer {
	return &Announcer{
		port:    port,
		getInfo: getInfo,
	}
}

// Run 开始周期性发送多播公告
func (a *Announcer) Run() {
	addr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("224.0.0.167:%d", a.port))
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		log.Printf("❌ Multicast init failed: %v", err)
		return
	}
	defer conn.Close()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	log.Println("📡 Multicast broadcaster started")

	// 启动突发广播 (100ms, 500ms, 2000ms)
	for _, d := range []time.Duration{100, 500, 2000} {
		time.Sleep(d * time.Millisecond)
		if a.getInfo != nil {
			data, _ := json.Marshal(a.getInfo())
			conn.Write(data)
		}
	}

	// 周期性广播
	for range ticker.C {
		if a.getInfo != nil {
			data, _ := json.Marshal(a.getInfo())
			conn.Write(data)
		}
	}
}
