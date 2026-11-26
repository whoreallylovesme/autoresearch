package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/whoreallylovesme/autoresearch/internal/api"
	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/research"
)

type daemonOptions struct {
	configPath       string
	listen           string
	dataDir          string
	allowNonLoopback bool
}

func main() {
	if err := newRootCommand().Execute(); err != nil {
		slog.Error("researchd stopped", "error", err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	var options daemonOptions
	command := &cobra.Command{Use: "researchd", Short: "Run the AutoResearch daemon", Args: cobra.NoArgs, SilenceErrors: true, SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error {
		return run(cmd.Context(), options)
	}}
	command.Flags().StringVar(&options.configPath, "config", "configs/autoresearch.yaml", "main configuration path")
	command.Flags().StringVar(&options.listen, "listen", "", "override listen address")
	command.Flags().StringVar(&options.dataDir, "data-dir", "", "override private runtime data directory")
	command.Flags().BoolVar(&options.allowNonLoopback, "allow-non-loopback", false, "allow non-loopback listen address behind trusted network controls")
	return command
}

func run(parent context.Context, options daemonOptions) error {
	configuration, err := config.Load(options.configPath)
	if err != nil {
		return err
	}
	if options.dataDir != "" {
		configuration.Runtime.DataDir = options.dataDir
	}
	address := configuration.Runtime.Listen
	if options.listen != "" {
		address = options.listen
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if !options.allowNonLoopback && host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return errors.New("listen address must be loopback unless --allow-non-loopback is explicit")
		}
	}
	if options.allowNonLoopback {
		slog.Warn("non-loopback listener enabled without built-in authentication; use trusted network controls")
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
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
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
