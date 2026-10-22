// Command server serves one PERF-01 arm on loopback until SIGTERM. It prints
// "READY <base URL>" once listening.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alswl/humanirvana/tests/performance/fixture"
)

func main() {
	arm := flag.String("arm", "", "native or adapter")
	listen := flag.String("listen", "127.0.0.1:0", "listen address")
	flag.Parse()
	if err := run(*arm, *listen); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arm, listen string) error {
	handler, err := fixture.NewHandler(arm)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	srv.SetKeepAlivesEnabled(true)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	fmt.Printf("READY http://%s\n", ln.Addr())

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, os.Interrupt)
	select {
	case err := <-errc:
		return err
	case <-stop:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}
