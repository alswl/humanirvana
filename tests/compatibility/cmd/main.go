package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	demo "github.com/alswl/humanirvana/tests/compatibility"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:18080", "HTTP listen address")
	mode := flag.String("mode", "migrated", "application route owner: original, migrated, rollback")
	flag.Parse()
	app, err := demo.New()
	if err != nil {
		log.Fatal(err)
	}
	entry, err := app.Entry(*mode)
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/original/", http.StripPrefix("/original", app.Original))
	mux.Handle("/migrated/", http.StripPrefix("/migrated", app.Migrated))
	mux.Handle("/app/", http.StripPrefix("/app", entry))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "TDD migration demo\n\nApplication owner: %s\n\nPOST /original/orders\nPOST /migrated/orders\nPOST /app/orders\nGET /app/orders/1\nGET /app/legacy\nGET /migrated/openapi.json\n\nAuthorization: Bearer demo-token\nJSON body: {\"item\":\"book\",\"quantity\":2}\n\nOriginal and migrated stores are isolated and reset on restart.\n", *mode)
	})
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result := make(chan error, 1)
	go func() { log.Printf("Demo: http://%s (mode=%s)", *listen, *mode); result <- server.ListenAndServe() }()
	select {
	case err := <-result:
		if err != http.ErrServerClosed {
			log.Fatal(err)
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			log.Print(err)
		}
	}
}
