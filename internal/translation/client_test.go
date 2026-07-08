package translation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/srt"
)

func TestAPIURL(t *testing.T) {
	cases := map[string]string{
		"http://host:8000":           "http://host:8000/v1/models",
		"http://host:8000/":          "http://host:8000/v1/models",
		"http://host:8000/v1":        "http://host:8000/v1/models",
		"http://host:8000/openai/v1": "http://host:8000/openai/v1/models",
	}
	for base, want := range cases {
		if got := APIURL(base, "/models"); got != want {
			t.Errorf("APIURL(%q) = %q, want %q", base, got, want)
		}
	}
}

func TestParseTranslations(t *testing.T) {
	var logs []string
	client := Client{Log: func(line string) { logs = append(logs, line) }}
	got := client.parseTranslations("```text\n[0] Olá\ncontinuação\n[9] ignore\n[1] Mundo\n```", []string{"Hello", "World", "Fallback"})
	want := []string{"Olá continuação", "Mundo", "Fallback"}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("got[%d] = %q, want %q", index, got[index], want[index])
		}
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "missing translation") {
		t.Fatalf("logs = %#v", logs)
	}
}

func TestParseTranslationsFromJSONPreservesFragments(t *testing.T) {
	client := Client{}
	got := client.parseTranslations(`{"translations":["Vou começar.","E eu sei que alguns de vocês estão treinando","em outra modalidade.","Mas outros estão começando.","Então vou garantir."]}`, []string{
		"I'm going to start.",
		"And I know some of you are training",
		"into another modality.",
		"But others are starting.",
		"So I will make sure.",
	})
	want := []string{
		"Vou começar.",
		"E eu sei que alguns de vocês estão treinando",
		"em outra modalidade.",
		"Mas outros estão começando.",
		"Então vou garantir.",
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("got[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestTranslateFileAgainstOpenAICompatibleServer(t *testing.T) {
	var modelCalls atomic.Int32
	var completionCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret" {
			http.Error(writer, "missing auth", http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/v1/models":
			modelCalls.Add(1)
			_ = json.NewEncoder(writer).Encode(map[string]any{"data": []map[string]string{{"id": "test-model"}}})
		case "/v1/chat/completions":
			completionCalls.Add(1)
			var payload struct {
				Model          string  `json:"model"`
				Temperature    float64 `json:"temperature"`
				MaxTokens      int     `json:"max_tokens"`
				ResponseFormat struct {
					Type string `json:"type"`
				} `json:"response_format"`
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				http.Error(writer, err.Error(), http.StatusBadRequest)
				return
			}
			if payload.Model != "test-model" || len(payload.Messages) != 1 || !strings.Contains(payload.Messages[0].Content, "Brazilian Portuguese") {
				http.Error(writer, "bad completion payload", http.StatusBadRequest)
				return
			}
			if payload.Temperature != 0 || payload.MaxTokens <= 0 || payload.MaxTokens >= 4096 || payload.ResponseFormat.Type != "json_object" {
				http.Error(writer, "bad generation controls", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "[0] Um\n[1] Dois"}}}})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	input := filepath.Join(dir, "input.srt")
	output := filepath.Join(dir, "output.srt")
	content := "1\n00:00:00,000 --> 00:00:01,000\nOne\n\n2\n00:00:01,000 --> 00:00:02,000\nTwo\n"
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	client := Client{APIBase: server.URL, APIKey: "secret", HTTPClient: server.Client()}
	if err := client.TranslateFile(context.Background(), input, output, "Brazilian Portuguese (pt-BR)", 10); err != nil {
		t.Fatal(err)
	}
	cues, err := srt.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != 2 || cues[0].Text != "Um" || cues[1].Text != "Dois" {
		t.Fatalf("translated cues = %#v", cues)
	}
	if modelCalls.Load() != 1 || completionCalls.Load() != 1 {
		t.Fatalf("calls: models=%d completions=%d", modelCalls.Load(), completionCalls.Load())
	}
}

func TestTranslateFileCachesAutoDetectedModel(t *testing.T) {
	var modelCalls atomic.Int32
	var completionCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/models":
			modelCalls.Add(1)
			_ = json.NewEncoder(writer).Encode(map[string]any{"data": []map[string]string{{"id": "cached-model"}}})
		case "/v1/chat/completions":
			completionCalls.Add(1)
			var payload struct {
				Model string `json:"model"`
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				http.Error(writer, err.Error(), http.StatusBadRequest)
				return
			}
			if payload.Model != "cached-model" {
				http.Error(writer, "unexpected model "+payload.Model, http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "[0] Um"}}}})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	content := "1\n00:00:00,000 --> 00:00:01,000\nOne\n"
	for index := 0; index < 2; index++ {
		input := filepath.Join(dir, "input-"+strconv.Itoa(index)+".srt")
		output := filepath.Join(dir, "output-"+strconv.Itoa(index)+".srt")
		if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		client := Client{APIBase: server.URL, HTTPClient: server.Client()}
		if err := client.TranslateFile(context.Background(), input, output, "Brazilian Portuguese (pt-BR)", 10); err != nil {
			t.Fatal(err)
		}
	}

	if modelCalls.Load() != 1 {
		t.Fatalf("model calls = %d, want cached single call", modelCalls.Load())
	}
	if completionCalls.Load() != 2 {
		t.Fatalf("completion calls = %d, want 2", completionCalls.Load())
	}
}

func TestTranslateFileRetriesTransientTranslationFailures(t *testing.T) {
	var completionCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(writer).Encode(map[string]any{"data": []map[string]string{{"id": "test-model"}}})
		case "/v1/chat/completions":
			call := completionCalls.Add(1)
			switch call {
			case 1:
				writer.Header().Set("Retry-After", "0")
				http.Error(writer, "temporary overload", http.StatusServiceUnavailable)
			case 2:
				writer.Header().Set("Retry-After", "0")
				http.Error(writer, "rate limited", http.StatusTooManyRequests)
			default:
				_ = json.NewEncoder(writer).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "[0] Um"}}}})
			}
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	input := filepath.Join(dir, "input.srt")
	output := filepath.Join(dir, "output.srt")
	content := "1\n00:00:00,000 --> 00:00:01,000\nOne\n"
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	client := Client{APIBase: server.URL, HTTPClient: server.Client()}
	if err := client.TranslateFile(context.Background(), input, output, "Brazilian Portuguese (pt-BR)", 10); err != nil {
		t.Fatal(err)
	}
	if completionCalls.Load() != 3 {
		t.Fatalf("completion calls = %d, want 3", completionCalls.Load())
	}
	cues, err := srt.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != 1 || cues[0].Text != "Um" {
		t.Fatalf("translated cues = %#v", cues)
	}
}

func TestTranslateFileRunsBatchesConcurrentlyInOutputOrder(t *testing.T) {
	var completionCalls atomic.Int32
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	firstBatchStarted := make(chan struct{})
	secondBatchStarted := make(chan struct{})
	var closeFirstBatchStarted sync.Once
	var closeSecondBatchStarted sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			http.NotFound(writer, request)
			return
		}
		current := inFlight.Add(1)
		for {
			maximum := maxInFlight.Load()
			if current <= maximum || maxInFlight.CompareAndSwap(maximum, current) {
				break
			}
		}
		defer inFlight.Add(-1)
		completionCalls.Add(1)
		var payload struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		if len(payload.Messages) != 1 {
			http.Error(writer, "missing prompt", http.StatusBadRequest)
			return
		}
		prompt := payload.Messages[0].Content
		switch {
		case strings.Contains(prompt, `"text":"One"`):
			closeFirstBatchStarted.Do(func() { close(firstBatchStarted) })
			select {
			case <-secondBatchStarted:
			case <-time.After(500 * time.Millisecond):
				writer.Header().Set("Retry-After", "0")
				http.Error(writer, "second batch did not start concurrently", http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"translations":["Um","Dois"]}`}}}})
		case strings.Contains(prompt, `"text":"Three"`):
			select {
			case <-firstBatchStarted:
			case <-time.After(500 * time.Millisecond):
				writer.Header().Set("Retry-After", "0")
				http.Error(writer, "first batch did not start concurrently", http.StatusServiceUnavailable)
				return
			}
			closeSecondBatchStarted.Do(func() { close(secondBatchStarted) })
			_ = json.NewEncoder(writer).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"translations":["Tres","Quatro"]}`}}}})
		default:
			http.Error(writer, "unexpected prompt", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	input := filepath.Join(dir, "input.srt")
	output := filepath.Join(dir, "output.srt")
	content := strings.Join([]string{
		"1\n00:00:00,000 --> 00:00:01,000\nOne",
		"2\n00:00:01,000 --> 00:00:02,000\nTwo",
		"3\n00:00:02,000 --> 00:00:03,000\nThree",
		"4\n00:00:03,000 --> 00:00:04,000\nFour",
		"",
	}, "\n\n")
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	client := Client{APIBase: server.URL, Model: "test-model", HTTPClient: server.Client()}
	if err := client.TranslateFile(context.Background(), input, output, "Brazilian Portuguese (pt-BR)", 2); err != nil {
		t.Fatal(err)
	}
	cues, err := srt.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(cues))
	for index, cue := range cues {
		got[index] = cue.Text
	}
	want := []string{"Um", "Dois", "Tres", "Quatro"}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("translated cue %d = %q, want %q; all cues=%#v", index, got[index], want[index], got)
		}
	}
	if completionCalls.Load() != 2 {
		t.Fatalf("completion calls = %d, want 2", completionCalls.Load())
	}
	if maxInFlight.Load() < 2 {
		t.Fatalf("max in-flight completion requests = %d, want at least 2", maxInFlight.Load())
	}
}

func TestPreflightChecksModelsEndpoint(t *testing.T) {
	var modelCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			http.NotFound(writer, request)
			return
		}
		modelCalls.Add(1)
		if request.Header.Get("Authorization") != "Bearer secret" {
			http.Error(writer, "missing auth", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"data": []map[string]string{{"id": "test-model"}}})
	}))
	defer server.Close()

	var logs []string
	client := Client{APIBase: server.URL, APIKey: "secret", HTTPClient: server.Client(), Log: func(line string) { logs = append(logs, line) }}
	model, err := client.Preflight(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if model != "test-model" {
		t.Fatalf("model = %q, want test-model", model)
	}
	if modelCalls.Load() != 1 {
		t.Fatalf("model calls = %d, want 1", modelCalls.Load())
	}
	if joined := strings.Join(logs, "\n"); !strings.Contains(joined, "Translation API reachable") {
		t.Fatalf("preflight logs missing reachability:\n%s", joined)
	}
}

func TestPreflightReportsAPIStatus(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		http.Error(writer, "bad key", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := Client{APIBase: server.URL, APIKey: "wrong", HTTPClient: server.Client()}
	_, err := client.Preflight(context.Background())
	if err == nil {
		t.Fatal("Preflight unexpectedly succeeded")
	}
	if text := err.Error(); !strings.Contains(text, "check translation API connectivity") || !strings.Contains(text, "HTTP 401") {
		t.Fatalf("preflight error = %q, want connectivity and HTTP status", text)
	}
	if calls.Load() != 1 {
		t.Fatalf("preflight calls = %d, want fail-fast without retry", calls.Load())
	}
}

func TestTranslateFileWritesEmptyOutputForEmptySRT(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "empty.srt")
	output := filepath.Join(dir, "empty.pt-BR.srt")
	if err := os.WriteFile(input, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var logs []string
	client := Client{APIBase: "http://127.0.0.1:1", Log: func(line string) { logs = append(logs, line) }}

	if err := client.TranslateFile(context.Background(), input, output, "Brazilian Portuguese (pt-BR)", 10); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("output = %q, want empty SRT", data)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "No subtitle entries") {
		t.Fatalf("logs = %#v", logs)
	}
}

func TestClientUsesConfiguredRequestTimeout(t *testing.T) {
	client := Client{RequestTimeout: 5 * time.Minute}

	got := client.client(120 * time.Second).Timeout

	if got != 5*time.Minute {
		t.Fatalf("client timeout = %s, want 5m0s", got)
	}
}

func TestValidateAPIBase(t *testing.T) {
	for _, base := range []string{"", "ftp://host", "http://", "http://host/path?q=x", "http://host/#fragment"} {
		if err := ValidateAPIBase(base); err == nil {
			t.Errorf("ValidateAPIBase(%q) unexpectedly succeeded", base)
		}
	}
	for _, base := range []string{"http://localhost:8000", "http://127.0.0.1:8000/v1", "http://[::1]:8000", "https://example.com/openai/v1"} {
		if err := ValidateAPIBase(base); err != nil {
			t.Errorf("ValidateAPIBase(%q) failed: %v", base, err)
		}
	}
	for _, base := range []string{"http://example.com", "http://10.0.0.25:8000", "http://192.168.1.10/v1"} {
		if err := ValidateAPIBase(base); err == nil || !strings.Contains(err.Error(), "HTTPS") {
			t.Errorf("ValidateAPIBase(%q) error = %v, want HTTPS requirement", base, err)
		}
	}
}
