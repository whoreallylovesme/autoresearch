package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/api"
	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/research"
)

func main() {
	if err := run(); err != nil {
		slog.Error("researchd stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	flags := flag.NewFlagSet("researchd", flag.ContinueOnError)
	configPath := flags.String("config", "configs/autoresearch.yaml", "main configuration path")
	listen := flags.String("listen", "", "override loopback listen address")
	dataDir := flags.String("data-dir", "", "override private runtime data directory")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	configuration, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if *dataDir != "" {
		configuration.Runtime.DataDir = *dataDir
	}
	address := configuration.Runtime.Listen
	if *listen != "" {
		address = *listen
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || (host != "localhost" && (net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback())) {
		return errors.New("listen address must be loopback")
	}
	runtime, err := research.NewPersistent(configuration, research.DefaultFactory())
	if err != nil {
		return err
	}
	defer runtime.Close()
	apiServer := api.New(runtime)
	httpServer := &http.Server{Addr: address, Handler: apiServer, ReadHeaderTimeout: 5 * time.Second}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	slog.Info("researchd ready", "address", address, "research", configuration.Name, "snapshot", configuration.Snapshot.Digest)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- httpServer.Serve(listener) }()
	select {
	case err := <-serveErrors:
		return err
	case <-ctx.Done():
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	apiServer.Shutdown(shutdownContext)
	return httpServer.Shutdown(shutdownContext)
}
