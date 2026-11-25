package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/research"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "researchctl:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: researchctl <validate|run|status|agents|tasks|budget|logs|stop|report> [flags]")
	}
	command, arguments := args[0], args[1:]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	apiURL := flags.String("api", "", "runtime API base URL")
	configPath := flags.String("config", "configs/autoresearch.yaml", "main configuration path")
	follow := flags.Bool("follow", false, "follow event stream")
	timeout := flags.Duration("timeout", 30*time.Minute, "maximum time to wait for research")
	message := flags.String("message", "", "operator chat message")
	directive := flags.Bool("directive", false, "send an authoritative operator directive")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if command == "validate" {
		configuration, err := config.Load(*configPath)
		if err != nil {
			return err
		}
		fmt.Printf("configuration valid: %s\nsnapshot: %s\n", configuration.Name, configuration.Snapshot.Digest)
		return nil
	}
	baseURL := *apiURL
	if command == "run" {
		configuration, err := config.Load(*configPath)
		if err != nil {
			return err
		}
		if baseURL == "" {
			baseURL = "http://" + configuration.Runtime.Listen
		}
		if err := call(ctx, http.MethodPost, baseURL+"/api/v1/run/start", nil, nil); err != nil {
			return fmt.Errorf("start research (is researchd running?): %w", err)
		}
		runContext, cancel := context.WithTimeout(ctx, *timeout)
		defer cancel()
		return waitForReport(runContext, baseURL)
	}
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8080"
	}
	switch command {
	case "status", "agents", "tasks", "budget", "report":
		path := map[string]string{"status": "/api/v1/run/status", "agents": "/api/v1/agents", "tasks": "/api/v1/tasks", "budget": "/api/v1/budget", "report": "/api/v1/report"}[command]
		var result any
		if err := call(ctx, http.MethodGet, baseURL+path, nil, &result); err != nil {
			return err
		}
		if command == "report" {
			fmt.Println(result.(map[string]any)["report"])
			return nil
		}
		return printJSON(result)
	case "stop", "pause", "resume":
		return call(ctx, http.MethodPost, baseURL+"/api/v1/run/"+command, nil, nil)
	case "finalize":
		var result any
		if err := call(ctx, http.MethodPost, baseURL+"/api/v1/run/finalize", nil, &result); err != nil {
			return err
		}
		return printJSON(result)
	case "chat":
		if *message == "" {
			return errors.New("chat requires --message")
		}
		payload, _ := json.Marshal(map[string]any{"message": *message, "directive": *directive})
		var result any
		if err := call(ctx, http.MethodPost, baseURL+"/api/v1/chat", payload, &result); err != nil {
			return err
		}
		return printJSON(result)
	case "logs":
		if *follow {
			return streamEvents(ctx, baseURL)
		}
		var result any
		if err := call(ctx, http.MethodGet, baseURL+"/api/v1/events", nil, &result); err != nil {
			return err
		}
		return printJSON(result)
	default:
		return fmt.Errorf("%s is not implemented in the minimal runtime", command)
	}
}

func waitForReport(ctx context.Context, baseURL string) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var status research.Status
		if err := call(ctx, http.MethodGet, baseURL+"/api/v1/run/status", nil, &status); err != nil {
			return err
		}
		switch status.State {
		case research.StateCompleted:
			var report map[string]string
			if err := call(ctx, http.MethodGet, baseURL+"/api/v1/report", nil, &report); err != nil {
				return err
			}
			fmt.Println(report["report"])
			return nil
		case research.StateFailed, research.StateCancelled:
			return fmt.Errorf("research ended in state %s", status.State)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func streamEvents(ctx context.Context, baseURL string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/events/stream", nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return responseError(response)
	}
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data: ") {
			fmt.Println(strings.TrimPrefix(scanner.Text(), "data: "))
		}
	}
	return scanner.Err()
}

func call(ctx context.Context, method, url string, body []byte, destination any) error {
	request, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return responseError(response)
	}
	if destination != nil {
		return json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(destination)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	return nil
}

func responseError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return fmt.Errorf("API returned %s: %s", response.Status, strings.TrimSpace(string(body)))
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
