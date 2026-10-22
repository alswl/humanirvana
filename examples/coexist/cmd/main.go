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

	"github.com/alswl/humanirvana/examples/coexist"
)

func main() {
	addr := flag.String("listen", "127.0.0.1:18082", "HTTP listen address (use :0 for a dynamic port)")
	arm := flag.String("arm", "composed", "original, adapted (RegisterLegacy), native (humago baseline), composed (Huma typed + legacy on Nirvana), or cutover")
	owner := flag.String("owner", "original", "cutover owner for POST /projects/{projectID}/tasks: original or adapted")
	observe := flag.Bool("observe", false, "test-only: serve GET /_demo/observe and, with -arm cutover, POST /_demo/cutover?owner=")
	flag.Parse()
	app, err := showcase.New()
	if err != nil {
		log.Fatal(err)
	}
	var handler http.Handler
	var cutover *showcase.RouteCutover
	if *arm == "cutover" {
		cutover = showcase.NewRouteCutover(app, http.MethodPost, "/projects/{projectID}/tasks")
		if err := cutover.SetOwner(*owner); err != nil {
			log.Fatal(err)
		}
		handler = cutover
		log.Printf("cutover selects POST /projects/{projectID}/tasks owner=%s; all other routes use Original", *owner)
	} else {
		if *owner != "original" {
			log.Fatal("-owner applies only to -arm cutover")
		}
		handler, err = app.Handler(*arm)
		if err != nil {
			log.Fatal(err)
		}
	}
	if *observe {
		handler = app.Observe(handler, cutover, *owner)
		log.Printf("test-only observation endpoints enabled under %s", showcase.ObservePrefix)
	}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() {
		log.Printf("Nirvana Showcase arm=%s owner=%s listening on http://%s", *arm, *owner, listener.Addr())
		done <- server.Serve(listener)
	}()
	select {
	case err := <-done:
		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	case <-ctx.Done():
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(c); err != nil {
			log.Print(err)
		}
	}
}
