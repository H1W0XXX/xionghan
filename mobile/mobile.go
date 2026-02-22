package mobile

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	httpserver "xionghan/internal/server/http"
)

// StartServer starts the local HTTP server.
// webDir: physical path to the extracted web assets
// modelPath: physical path to the extracted .onnx model
// libPath: physical path to the libonnxruntime.so
// port: port to listen on, e.g. "2888"
func StartServer(webDir string, modelPath string, libPath string, port string) {
	mux := http.NewServeMux()
	h := httpserver.NewHandler()
	desktopDir, mobileDir := resolveAssetDirs(webDir)

	if modelPath != "" {
		if err := h.Engine().InitNN(modelPath, libPath); err != nil {
			log.Printf("Failed to initialize NN: %v", err)
		}
	}

	mux.Handle("/api/", h)
	httpserver.RegisterStaticRoutes(mux, desktopDir, mobileDir)
	log.Printf("Serving desktop UI from %s, mobile UI from %s", desktopDir, mobileDir)

	// Run in background so it doesn't block the Android UI thread
	go func() {
		if err := http.ListenAndServe("127.0.0.1:"+port, mux); err != nil {
			log.Printf("Server Error: %v", err)
		}
	}()
}

func resolveAssetDirs(webDir string) (string, string) {
	desktopDir := webDir
	mobileDir := webDir
	if webDir == "" {
		return desktopDir, mobileDir
	}

	// Case 1: webDir is an asset root that contains both subdirs.
	rootDesktop := filepath.Join(webDir, "web")
	rootMobile := filepath.Join(webDir, "web_mobile")
	if isDir(rootDesktop) || isDir(rootMobile) {
		if isDir(rootDesktop) {
			desktopDir = rootDesktop
		}
		if isDir(rootMobile) {
			mobileDir = rootMobile
		}
		if !isDir(mobileDir) {
			mobileDir = desktopDir
		}
		if !isDir(desktopDir) {
			desktopDir = mobileDir
		}
		return desktopDir, mobileDir
	}

	// Case 2: webDir points at web or web_mobile, try sibling.
	base := strings.ToLower(filepath.Base(webDir))
	parent := filepath.Dir(webDir)
	switch base {
	case "web":
		candidate := filepath.Join(parent, "web_mobile")
		if isDir(candidate) {
			mobileDir = candidate
		}
	case "web_mobile":
		candidate := filepath.Join(parent, "web")
		if isDir(candidate) {
			desktopDir = candidate
		}
	}
	return desktopDir, mobileDir
}

func isDir(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}
