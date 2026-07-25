package admin

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

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
	// 每次请求实时读取接收目录，避免运行时修改接收目录后下载链接失效
	mux.Handle("/files/", http.StripPrefix("/files/", s.serveFiles()))

	addr := fmt.Sprintf("0.0.0.0:%d", s.port)
	log.Printf("🛡️ Admin Panel listening on http://%s", addr)

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	select {
	case err := <-errCh:
		log.Fatalf("❌ Admin Service failed: %v", err)
	case <-sigCh:
		log.Println("🛑 Admin Service shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Fatalf("❌ Admin Service shutdown error: %v", err)
		}
		s.state.CloseDB()
		log.Println("✅ Admin Service stopped.")
	}
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
		if err := os.MkdirAll(req.ReceiveDir, 0755); err != nil {
			http.Error(w, "Cannot create directory: "+err.Error(), 400)
			return
		}
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

// serveFiles 返回一个每次请求都实时读取当前接收目录的文件服务 handler
// http.Dir 会清理路径并阻止 ".." 穿越，安全性与 http.FileServer 一致
func (s *Server) serveFiles() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.FileServer(http.Dir(s.state.GetReceiveDir())).ServeHTTP(w, r)
	})
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	dir := s.state.GetReceiveDir()

	q := r.URL.Query()
	keyword := strings.ToLower(strings.TrimSpace(q.Get("keyword")))
	typeFilter := strings.ToLower(strings.TrimPrefix(q.Get("type"), "."))
	fromTime, hasFrom := parseFilterDate(q.Get("from"), false)
	toTime, hasTo := parseFilterDate(q.Get("to"), true)

	type fileItem struct {
		name    string
		relPath string
		size    int64
		modTime time.Time
	}

	var all []fileItem
	var grandTotal int
	var grandSize int64
	typeSet := map[string]struct{}{}

	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		relPath, _ := filepath.Rel(dir, path)
		grandTotal++
		grandSize += info.Size()

		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(d.Name()), "."))
		if ext != "" {
			typeSet[ext] = struct{}{}
		}

		// 应用过滤条件
		if keyword != "" && !strings.Contains(strings.ToLower(d.Name()), keyword) {
			return nil
		}
		if typeFilter != "" && ext != typeFilter {
			return nil
		}
		if hasFrom && info.ModTime().Before(fromTime) {
			return nil
		}
		if hasTo && !info.ModTime().Before(toTime) {
			return nil
		}

		all = append(all, fileItem{
			name:    d.Name(),
			relPath: relPath,
			size:    info.Size(),
			modTime: info.ModTime(),
		})
		return nil
	})

	// 按修改时间倒序 (最新在前)
	sort.Slice(all, func(i, j int) bool {
		return all[i].modTime.After(all[j].modTime)
	})

	total := len(all)

	// 分页参数
	page := atoiDefault(q.Get("page"), 1)
	if page < 1 {
		page = 1
	}
	pageSize := atoiDefault(q.Get("pageSize"), 20)
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 500 {
		pageSize = 500
	}

	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	items := []map[string]interface{}{}
	for _, f := range all[start:end] {
		items = append(items, map[string]interface{}{
			"name":    f.name,
			"path":    f.relPath,
			"size":    f.size,
			"modTime": f.modTime.Format(time.RFC3339),
			"url":     "/files/" + filepath.ToSlash(f.relPath),
		})
	}

	availableTypes := make([]string, 0, len(typeSet))
	for t := range typeSet {
		availableTypes = append(availableTypes, t)
	}
	sort.Strings(availableTypes)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"items":          items,
		"total":          total,
		"page":           page,
		"pageSize":       pageSize,
		"availableTypes": availableTypes,
		"grandTotal":     grandTotal,
		"grandSize":      grandSize,
	})
}

// parseFilterDate 解析 YYYY-MM-DD 过滤日期 (按本地时区)
// endOfDay=true 时返回次日零点，用于 "到" 的开区间上界 (含当天)
func parseFilterDate(v string, endOfDay bool) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02", v, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	if endOfDay {
		t = t.AddDate(0, 0, 1)
	}
	return t, true
}

// atoiDefault 解析整数，失败时返回默认值
func atoiDefault(v string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
		return n
	}
	return def
}
