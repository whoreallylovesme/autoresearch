package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/operatorauth"
	"github.com/whoreallylovesme/autoresearch/internal/research"
)

type apiClient struct {
	token      operatorauth.Token
	httpClient *http.Client
}

func newAPIClient() (*apiClient, error) {
	token, err := operatorauth.Load()
	if err != nil {
		return nil, err
	}
	return &apiClient{token: token, httpClient: &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return errors.New("refusing to forward operator credentials through an API redirect")
	}}}, nil
}

func (client *apiClient) waitForReport(ctx context.Context, baseURL string, output io.Writer) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var status research.Status
		if err := client.call(ctx, http.MethodGet, baseURL+"/api/v1/run/status", nil, &status); err != nil {
			return err
		}
		switch status.State {
		case research.StateCompleted:
			var report map[string]string
			if err := client.call(ctx, http.MethodGet, baseURL+"/api/v1/report", nil, &report); err != nil {
				return err
			}
			fmt.Fprintln(output, report["report"])
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

func (client *apiClient) streamEvents(ctx context.Context, baseURL string, output io.Writer) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/events/stream", nil)
	if err != nil {
		return err
	}
	if err := client.token.SetBearer(request); err != nil {
		return err
	}
	response, err := client.httpClient.Do(request)
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
			fmt.Fprintln(output, strings.TrimPrefix(scanner.Text(), "data: "))
		}
	}
	return scanner.Err()
}

func (client *apiClient) call(ctx context.Context, method, url string, body []byte, destination any) error {
	request, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if err := client.token.SetBearer(request); err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
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
