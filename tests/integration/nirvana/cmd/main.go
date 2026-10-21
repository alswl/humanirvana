package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	demopkg "github.com/alswl/humanirvana/tests/integration/nirvana"
	nirvanalog "github.com/caicloud/nirvana/log"
)

func main() {
	port := uint16(8081)
	if envPort := os.Getenv("PORT"); envPort != "" {
		var p int
		fmt.Sscanf(envPort, "%d", &p)
		port = uint16(p)
	}

	server := demopkg.NewServer(port)

	nirvanalog.DefaultLogger().Infof("Starting Nirvana demo server on port %d", port)

	go func() {
		if err := server.Serve(); err != nil && err != http.ErrServerClosed {
			nirvanalog.DefaultLogger().Errorf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	nirvanalog.DefaultLogger().Info("Shutting down server...")
}
