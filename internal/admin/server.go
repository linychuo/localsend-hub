package admin

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"localsend-hub/internal/state"
)

//go:embed web/*
var webFS embed.FS

// Server 管理面板服务
type Server struct {
	state state.AdminStateProvider
	port  int
}

// NewServer 创建管理面板服务实例
func NewServer(st state.AdminStateProvider, port int) *Server {
	return &Server{state: st, port: port}
}

// Start 启动本地 HTTP 管理面板
func (s *Server) Start() {
	mux := http.NewServeMux()

	// API 路由
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/identity", s.handleIdentity)
	mux.HandleFunc("/api/files", s.handleFiles)

	// Web UI 路由 - 提供静态文件
	webSub, _ := fs.Sub(webFS, "web")
	fileServer := http.FileServer(http.FS(webSub))
	mux.Handle("/", fileServer)

	// 静态文件代理 (让前端能下载文件)
	mux.Handle("/files/", http.StripPrefix("/files/", http.FileServer(http.Dir(s.state.GetReceiveDir()))))

	addr := fmt.Sprintf("0.0.0.0:%d", s.port)
	log.Printf("🛡️ Admin Panel listening on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		s.state.ClearLogs()
		w.Write([]byte(`{"ok":true}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.state.GetLogs())
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"receiveDir": s.state.GetReceiveDir(),
		})
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}

	var req struct{ ReceiveDir string `json:"receiveDir"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad Request", 400)
		return
	}

	if req.ReceiveDir != "" {
		os.MkdirAll(req.ReceiveDir, 0755)
		s.state.SetReceiveDir(req.ReceiveDir)
		log.Println("📁 Receive directory updated:", req.ReceiveDir)
	}
	w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleIdentity(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct {
			Alias       string `json:"alias"`
			DeviceModel string `json:"deviceModel"`
			DeviceType  string `json:"deviceType"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad Request", 400)
			return
		}

		s.state.SetDeviceIdentity(req.Alias, req.DeviceModel, req.DeviceType)
		log.Printf("🆔 Identity updated: %s / %s / %s", req.Alias, req.DeviceModel, req.DeviceType)
		w.Write([]byte(`{"ok":true}`))
		return
	}

	if r.Method == http.MethodGet {
		alias, model, deviceType := s.state.GetDeviceIdentity()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"alias":       alias,
			"deviceModel": model,
			"deviceType":  deviceType,
		})
		return
	}

	http.Error(w, "Method not allowed", 405)
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	dir := s.state.GetReceiveDir()
	var res []map[string]interface{}

	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, _ := d.Info()
		// 计算相对于接收目录的路径
		relPath, _ := filepath.Rel(dir, path)
		res = append(res, map[string]interface{}{
			"name":    d.Name(),
			"path":    relPath,
			"size":    info.Size(),
			"modTime": info.ModTime().Format("2006-01-02 15:04:05"),
			"url":     "/files/" + filepath.ToSlash(relPath),
		})
		return nil
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}
