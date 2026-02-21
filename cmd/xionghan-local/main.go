package main

import (
	"flag"
	"log"
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

func main() {
	addr := flag.String("addr", ":2888", "listen address")
	webDir := flag.String("web", "./web", "directory with index.html / js / svg")
	modelPath := flag.String("model", "xionghan.onnx", "path to ONNX model file")
	libPath := flag.String("lib", "onnxruntime.dll", "path to onnxruntime.dll")
	flag.Parse()

	mux := http.NewServeMux()
	*webDir = resolveExistingDir(*webDir)

	h := httpserver.NewHandler()

	if *modelPath != "" {
		log.Printf("Initializing NN with model %s and lib %s", *modelPath, *libPath)
		if err := h.Engine().InitNN(*modelPath, *libPath); err != nil {
			log.Fatalf("Failed to initialize NN: %v", err)
		}
	}

	mux.Handle("/api/", h)

	fileServer := http.FileServer(http.Dir(*webDir))
	mux.Handle("/", fileServer)

	log.Printf("listening on %s, serving static from %s", *addr, *webDir)

	// ⭐ 延迟 100ms 打开默认浏览器，否则可能服务器未启动完成
	go func() {
		time.Sleep(100 * time.Millisecond)
		openBrowser("http://127.0.0.1" + *addr)
	}()

	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}
