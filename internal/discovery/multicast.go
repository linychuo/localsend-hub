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
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("224.0.0.167:%d", a.port))
	if err != nil {
		log.Printf("❌ Multicast addr resolve failed: %v", err)
		return
	}

	conns := a.dialAll(addr)
	if len(conns) == 0 {
		log.Printf("❌ Multicast init failed: no usable interface")
		return
	}
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	log.Printf("📡 Multicast broadcaster started on %d interface(s)", len(conns))

	// 启动突发广播 (100ms, 500ms, 2000ms)
	for _, d := range []time.Duration{100, 500, 2000} {
		time.Sleep(d * time.Millisecond)
		a.broadcast(conns)
	}

	// 周期性广播
	for range ticker.C {
		a.broadcast(conns)
	}
}

// dialAll 为每个 up 且支持多播的网卡建立一个到多播组的 UDP 连接
// 绑定网卡自身的 IPv4 作为源地址，内核据此从对应网卡发出多播，覆盖多网卡主机
// 若枚举失败或无可用网卡，回退到默认路由的单连接
func (a *Announcer) dialAll(raddr *net.UDPAddr) []*net.UDPConn {
	var conns []*net.UDPConn

	ifaces, err := net.Interfaces()
	if err != nil {
		ifaces = nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, ad := range addrs {
			ipNet, ok := ad.(*net.IPNet)
			if !ok || ipNet.IP.To4() == nil {
				continue
			}
			laddr := &net.UDPAddr{IP: ipNet.IP.To4()}
			conn, err := net.DialUDP("udp", laddr, raddr)
			if err != nil {
				log.Printf("⚠️ Multicast dial on %s failed: %v", iface.Name, err)
				continue
			}
			conns = append(conns, conn)
			break // 每张网卡一个 IPv4 即可
		}
	}

	// 回退：没有枚举到可用网卡时用默认路由
	if len(conns) == 0 {
		if conn, err := net.DialUDP("udp", nil, raddr); err == nil {
			conns = append(conns, conn)
		}
	}
	return conns
}

// broadcast 向所有网卡连接发送一次设备信息公告
func (a *Announcer) broadcast(conns []*net.UDPConn) {
	if a.getInfo == nil {
		return
	}
	data, err := json.Marshal(a.getInfo())
	if err != nil {
		return
	}
	for _, c := range conns {
		c.Write(data)
	}
}
