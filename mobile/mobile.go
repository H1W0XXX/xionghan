package mobile

import (
	"log"
	"net/http"
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

	if modelPath != "" {
		if err := h.Engine().InitNN(modelPath, libPath); err != nil {
			log.Printf("Failed to initialize NN: %v", err)
		}
	}

	mux.Handle("/api/", h)
	mux.Handle("/", http.FileServer(http.Dir(webDir)))

	// Run in background so it doesn't block the Android UI thread
	go func() {
		if err := http.ListenAndServe("127.0.0.1:"+port, mux); err != nil {
			log.Printf("Server Error: %v", err)
		}
	}()
}
