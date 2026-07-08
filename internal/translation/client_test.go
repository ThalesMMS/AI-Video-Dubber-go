package translation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
		if err := validateAPIBase(base); err == nil {
			t.Errorf("validateAPIBase(%q) unexpectedly succeeded", base)
		}
	}
}
