package main

import (
	"localsend-hub/internal/admin"
	"localsend-hub/internal/state"
	"log"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("🛡️ LocalSend Admin Starting...")

	// 初始化管理服务状态 (从配置文件读取，独立于核心服务)
	adminState := state.NewAdminState()

	log.Printf("📁 Receive Directory: %s", adminState.GetReceiveDir())
	log.Printf("🌐 Core Port: %d (HTTPS)", adminState.GetCorePort())
	log.Printf("🛡️ Admin Console Port: %d", adminState.GetAdminPort())

	// 启动管理服务 (独立进程)
	adminServer := admin.NewServer(adminState, adminState.GetAdminPort())
	
	log.Println("✅ Admin Service Ready!")
	
	// Start() 是阻塞的，如果失败会 log.Fatal
	adminServer.Start()
}
