package main

// A simple HTTP server that mimics an update server.
// The directorory structure under the "dist" directory is served on on port 80.

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	baseDir    = "dist"
	listenAddr = ":80"
)

func checkDirAccess(dir string) error {
	// Try to open and read the directory
	filehandle, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer filehandle.Close()

	// Try to read directory entries
	_, err = filehandle.Readdirnames(1)
	if err != nil && err.Error() != "EOF" {
		return err
	}

	return nil
}

// loggingMiddleware wraps an http.Handler and logs each request.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		log.Printf("%s %s %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
		log.Printf("%s %s completed in %v", r.Method, r.URL.Path, time.Since(start))
	})
}

func main() {
	outDir, err := filepath.Abs(baseDir)
	if err != nil {
		log.Fatalf("Failed to get absolute path: %v", err)
	}

	// Check if directory exists
	info, err := os.Stat(outDir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Fatalf("Directory does not exist: %s", outDir)
		}

		log.Fatalf("Failed to stat directory: %v", err)
	}

	// Check if it's a directory
	if !info.IsDir() {
		log.Fatalf("Path is not a directory: %s", outDir)
	}

	// Check if directory is readable/executable
	err = checkDirAccess(outDir)
	if err != nil {
		log.Fatalf("Directory is not accessible: %v", err)
	}

	// Create file server
	fs := http.FileServer(http.Dir(outDir))

	// Handle all paths with logging
	http.Handle("/", loggingMiddleware(fs))

	// Start server on port 80
	log.Printf("Starting server on %s, serving files from %s", listenAddr, outDir)

	server := &http.Server{
		Addr:         listenAddr,
		Handler:      nil,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 600 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	err = server.ListenAndServe()
	if err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
