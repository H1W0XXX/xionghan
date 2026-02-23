package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	// _ "net/http/pprof"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	httpserver "xionghan/internal/server/http"
)

func openBrowser(url string) {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default: // linux / bsd
		cmd = exec.Command("xdg-open", url)
	}

	_ = cmd.Start() // 不阻塞，不关心错误（某些服务器环境可能无图形界面）
}

func resolveExistingDir(dir string) string {
	if dir == "" {
		return dir
	}
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		if abs, e := filepath.Abs(dir); e == nil {
			return abs
		}
		return dir
	}
	if filepath.IsAbs(dir) {
		return dir
	}
	exe, err := os.Executable()
	if err != nil {
		return dir
	}
	exeDir := filepath.Dir(exe)
	candidates := []string{
		filepath.Join(exeDir, dir),
		filepath.Join(exeDir, filepath.Base(dir)),
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return dir
}

func isDir(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

func main() {
	addr := flag.String("addr", "0.0.0.0:2888", "listen address")
	webDir := flag.String("web", "./web", "directory with index.html / js / svg")
	webMobileDir := flag.String("web-mobile", "./web_mobile", "directory with mobile index.html / js / svg")
	modelPath := flag.String("model", "xionghan.onnx", "path to ONNX model file")
	libPath := flag.String("lib", "onnxruntime.dll", "path to onnxruntime.dll")
	flag.Parse()

	mux := http.NewServeMux()
	*webDir = resolveExistingDir(*webDir)
	*webMobileDir = resolveExistingDir(*webMobileDir)
	if !isDir(*webDir) {
		log.Printf("warning: desktop web dir does not exist: %s", *webDir)
	}
	if !isDir(*webMobileDir) {
		log.Printf("warning: mobile web dir does not exist: %s, fallback to desktop dir", *webMobileDir)
		*webMobileDir = *webDir
	}

	h := httpserver.NewHandler()

	if *modelPath != "" {
		log.Printf("Initializing NN with model %s and lib %s", *modelPath, *libPath)
		if err := h.Engine().InitNN(*modelPath, *libPath); err != nil {
			log.Fatalf("Failed to initialize NN: %v", err)
		}
	}

	mux.Handle("/api/", h)
	httpserver.RegisterStaticRoutes(mux, *webDir, *webMobileDir)

	log.Printf("listening on %s, serving desktop static from %s, mobile static from %s", *addr, *webDir, *webMobileDir)

	// ⭐ 延迟 100ms 打开默认浏览器，否则可能服务器未启动完成
	go func() {
		time.Sleep(100 * time.Millisecond)
		// 自动从 addr 提取端口部分，例如 "0.0.0.0:2888" -> "2888"
		_, port, err := net.SplitHostPort(*addr)
		if err != nil {
			// 如果 addr 格式不标准（比如只有 ":2888"），降级处理
			openBrowser("http://127.0.0.1:2888")
		} else {
			openBrowser("http://127.0.0.1:" + port)
		}
	}()

	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}
