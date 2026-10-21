package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alswl/humanirvana/examples/basic"
)

func main() {
	addr := flag.String("listen", "127.0.0.1:0", "HTTP listen address; :0 selects a dynamic port")
	mode := flag.String("mode", "native", "native or mixed")
	flag.Parse()
	h, err := reviewcombination.Handler(*mode)
	if err != nil {
		log.Fatal(err)
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	log.Printf("mode=%s url=http://%s", *mode, ln.Addr())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-done:
		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			log.Fatal(err)
		}
	}
}
