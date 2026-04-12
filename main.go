package main

import (
	"localsend-hub/internal/core"
	"localsend-hub/internal/state"
	"log"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("🚀 LocalSend Hub Starting (Core Service)...")

	// 1. 初始化核心服务状态 (加载配置或创建默认配置)
	// 这个操作会自动读取 localsend_config.json，如果不存在则创建默认值
	st := state.New()

	// 2. 启动核心服务 (使用配置中的端口)
	coreServer := core.NewServer(st, st.CorePort)
	
	log.Println("✅ Core Service Ready!")
	
	// Start() 是阻塞的，如果失败会返回错误
	if err := coreServer.Start(); err != nil {
		log.Fatalf("❌ Core Service failed: %v", err)
	}
}
