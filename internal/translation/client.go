// Package translation translates SRT cues through an OpenAI-compatible API.
package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/srt"
)

var taggedLine = regexp.MustCompile(`^\[(\d+)]\s*(.*)$`)

const (
	defaultHTTPMaxRetries   = 2
	defaultHTTPRetryBackoff = 500 * time.Millisecond
	maxHTTPRetryBackoff     = 10 * time.Second
	resolvedModelCacheTTL   = 5 * time.Minute
)

type resolvedModelCacheEntry struct {
	Model   string
	Expires time.Time
}

var resolvedModelCache = struct {
	sync.Mutex
	entries map[string]resolvedModelCacheEntry
}{entries: make(map[string]resolvedModelCacheEntry)}

// LogFunc receives progress and warning messages.
type LogFunc func(string)

// Client calls an OpenAI-compatible /v1 API.
type Client struct {
	APIBase        string
	APIKey         string
	Model          string
	RequestTimeout time.Duration
	HTTPClient     *http.Client
	Log            LogFunc
}

// Preflight checks that the translation API is reachable before local work starts.
// It returns the configured model, or the first listed model when auto-detection is needed.
func (c *Client) Preflight(ctx context.Context) (string, error) {
	if err := ValidateAPIBase(c.APIBase); err != nil {
		return "", err
	}
	models, err := c.listModels(ctx)
	if err != nil {
		return "", fmt.Errorf("check translation API connectivity: %w", err)
	}
	if len(models) == 0 {
		return "", fmt.Errorf("check translation API connectivity: the API returned no models")
	}
	model := strings.TrimSpace(c.Model)
	if model == "" {
		model = models[0]
		cacheResolvedModel(c.APIBase, model)
		c.logf("Auto-detected model: %s", model)
	}
	c.logf("Translation API reachable: %s", strings.TrimRight(c.APIBase, "/"))
	return model, nil
}

// TranslateFile translates the text of each SRT cue while preserving timing.
func (c *Client) TranslateFile(ctx context.Context, inputPath, outputPath, targetLanguage string, batchSize int) error {
	if err := ValidateAPIBase(c.APIBase); err != nil {
		return err
	}
	if strings.TrimSpace(targetLanguage) == "" {
		return fmt.Errorf("target language is empty")
	}
	inputData, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("read SRT %q: %w", inputPath, err)
	}
	if strings.TrimSpace(string(inputData)) == "" {
		c.logf("No subtitle entries in %s; writing empty translated SRT.", inputPath)
		return srt.WriteFile(outputPath, nil)
	}
	cues, err := srt.ReadFile(inputPath)
	if err != nil {
		return err
	}
	if batchSize <= 0 {
		batchSize = 15
	}
	model, err := c.resolveModel(ctx)
	if err != nil {
		c.logf("Warning: model auto-detection failed (%v); using 'default'.", err)
		model = "default"
	}
	c.logf("API: %s  Model: %s", strings.TrimRight(c.APIBase, "/"), model)
	c.logf("Parsed %d subtitle entries from %s", len(cues), inputPath)
	c.logf("Target language: %s", targetLanguage)

	for start := 0; start < len(cues); start += batchSize {
		end := start + batchSize
		if end > len(cues) {
			end = len(cues)
		}
		texts := make([]string, end-start)
		for index := range texts {
			texts[index] = cues[start+index].Text
		}
		c.logf("Translating entries %d–%d of %d...", start+1, end, len(cues))
		translated, err := c.translateBatch(ctx, model, targetLanguage, texts)
		if err != nil {
			return fmt.Errorf("translate subtitle batch %d-%d: %w", start+1, end, err)
		}
		for index, text := range translated {
			cues[start+index].Text = text
		}
	}
	if err := srt.WriteFile(outputPath, cues); err != nil {
		return err
	}
	c.logf("Done → %s", outputPath)
	return nil
}

func (c *Client) resolveModel(ctx context.Context) (string, error) {
	if model := strings.TrimSpace(c.Model); model != "" {
		return model, nil
	}
	if model, ok := cachedResolvedModel(c.APIBase); ok {
		c.logf("Auto-detected model: %s (cached)", model)
		return model, nil
	}
	models, err := c.listModels(ctx)
	if err != nil {
		return "", err
	}
	if len(models) == 0 {
		return "", fmt.Errorf("the API returned no models")
	}
	cacheResolvedModel(c.APIBase, models[0])
	c.logf("Auto-detected model: %s", models[0])
	return models[0], nil
}

func cachedResolvedModel(apiBase string) (string, bool) {
	key := resolvedModelCacheKey(apiBase)
	now := time.Now()
	resolvedModelCache.Lock()
	defer resolvedModelCache.Unlock()
	entry, ok := resolvedModelCache.entries[key]
	if !ok {
		return "", false
	}
	if !now.Before(entry.Expires) {
		delete(resolvedModelCache.entries, key)
		return "", false
	}
	return entry.Model, true
}

func cacheResolvedModel(apiBase, model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	key := resolvedModelCacheKey(apiBase)
	resolvedModelCache.Lock()
	defer resolvedModelCache.Unlock()
	resolvedModelCache.entries[key] = resolvedModelCacheEntry{Model: model, Expires: time.Now().Add(resolvedModelCacheTTL)}
}

func resolvedModelCacheKey(apiBase string) string {
	return strings.TrimRight(strings.TrimSpace(apiBase), "/")
}

func (c *Client) listModels(ctx context.Context) ([]string, error) {
	response, err := c.doAPI(ctx, http.MethodGet, "models", nil, 10*time.Second, "")
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, responseError(response)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if model := strings.TrimSpace(item.ID); model != "" {
			models = append(models, model)
		}
	}
	return models, nil
}

func (c *Client) translateBatch(ctx context.Context, model, targetLanguage string, texts []string) ([]string, error) {
	items := make([]map[string]any, len(texts))
	for index, text := range texts {
		items[index] = map[string]any{"id": index, "text": strings.TrimSpace(text)}
	}
	inputJSON, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	prompt := "You are a professional subtitle translator. " +
		"Translate each English subtitle item to " + targetLanguage + ". " +
		"Return ONLY valid JSON with this exact shape: {\"translations\":[\"...\"]}. " +
		"The translations array MUST contain exactly " + strconv.Itoa(len(texts)) + " strings, in the same order as the input items. " +
		"Do not combine, split, omit, or renumber items. Translate each item independently, even when it is a sentence fragment. " +
		"Do not add explanations, markdown, numbering, or extra keys.\n\nInput JSON:\n" + string(inputJSON)

	payload := map[string]any{
		"model":           model,
		"messages":        []map[string]string{{"role": "user", "content": prompt}},
		"temperature":     0.0,
		"max_tokens":      translationMaxTokens(texts),
		"response_format": map[string]string{"type": "json_object"},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	response, err := c.doAPI(ctx, http.MethodPost, "chat/completions", encoded, 120*time.Second, "application/json")
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, responseError(response)
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(&completion); err != nil {
		return nil, fmt.Errorf("decode completion: %w", err)
	}
	if len(completion.Choices) == 0 {
		return nil, fmt.Errorf("the API returned no completion choices")
	}
	content, err := decodeContent(completion.Choices[0].Message.Content)
	if err != nil {
		return nil, err
	}
	return c.parseTranslations(content, texts), nil
}

func decodeContent(raw json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text), nil
	}
	// Some OpenAI-compatible servers return typed content parts.
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var builder strings.Builder
		for _, part := range parts {
			if part.Text != "" {
				builder.WriteString(part.Text)
			}
		}
		if builder.Len() > 0 {
			return strings.TrimSpace(builder.String()), nil
		}
	}
	return "", fmt.Errorf("completion content was not textual")
}

func (c *Client) parseTranslations(reply string, originals []string) []string {
	reply = stripCodeFence(reply)
	if translated, ok := parseJSONTranslations(reply, originals); ok {
		return translated
	}

	parsed := make(map[int]string, len(originals))
	current := -1
	for _, rawLine := range strings.Split(strings.ReplaceAll(reply, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		match := taggedLine.FindStringSubmatch(line)
		if match != nil {
			index, err := strconv.Atoi(match[1])
			if err != nil || index < 0 || index >= len(originals) {
				current = -1
				continue
			}
			current = index
			parsed[index] = strings.TrimSpace(match[2])
			continue
		}
		if current >= 0 {
			parsed[current] = strings.TrimSpace(parsed[current] + " " + line)
		}
	}

	translated := make([]string, len(originals))
	for index, original := range originals {
		if value := strings.TrimSpace(parsed[index]); value != "" {
			translated[index] = value
		} else {
			c.logf("WARNING: missing translation for batch item [%d], keeping original", index)
			translated[index] = original
		}
	}
	return translated
}

func parseJSONTranslations(reply string, originals []string) ([]string, bool) {
	var object struct {
		Translations []string `json:"translations"`
	}
	if err := json.Unmarshal([]byte(reply), &object); err == nil && len(object.Translations) == len(originals) {
		return cleanTranslations(object.Translations), true
	}

	var array []string
	if err := json.Unmarshal([]byte(reply), &array); err == nil && len(array) == len(originals) {
		return cleanTranslations(array), true
	}

	return nil, false
}

func cleanTranslations(values []string) []string {
	translated := make([]string, len(values))
	for index, value := range values {
		translated[index] = strings.TrimSpace(value)
	}
	return translated
}

func translationMaxTokens(texts []string) int {
	tokens := 128 + len(texts)*96
	if tokens < 256 {
		return 256
	}
	if tokens > 4096 {
		return 4096
	}
	return tokens
}

func stripCodeFence(reply string) string {
	reply = strings.TrimSpace(reply)
	if !strings.HasPrefix(reply, "```") {
		return reply
	}
	lines := strings.Split(strings.ReplaceAll(reply, "\r\n", "\n"), "\n")
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		lines = lines[1:]
	}
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// ValidateAPIBase checks the shared CLI/GUI rules for OpenAI-compatible API base URLs.
func ValidateAPIBase(base string) error {
	base = strings.TrimSpace(base)
	parsed, err := url.Parse(base)
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("invalid OpenAI-compatible API base URL %q", base)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("API base URL must not contain a query string or fragment: %q", base)
	}
	if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
		return fmt.Errorf("HTTPS is required for non-loopback OpenAI-compatible API base URL %q", base)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// APIURL appends an OpenAI route without duplicating /v1.
func APIURL(base, route string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	route = strings.TrimLeft(route, "/")
	if strings.HasSuffix(strings.ToLower(base), "/v1") {
		return base + "/" + route
	}
	return base + "/v1/" + route
}

func (c *Client) setHeaders(request *http.Request) {
	if key := strings.TrimSpace(c.APIKey); key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
}

func (c *Client) client(timeout time.Duration) *http.Client {
	if c.RequestTimeout > 0 {
		timeout = c.RequestTimeout
	}
	if c.HTTPClient != nil {
		clone := *c.HTTPClient
		if clone.Timeout == 0 {
			clone.Timeout = timeout
		}
		return &clone
	}
	return &http.Client{Timeout: timeout}
}

func (c *Client) doAPI(ctx context.Context, method, route string, body []byte, timeout time.Duration, contentType string) (*http.Response, error) {
	client := c.client(timeout)
	url := APIURL(c.APIBase, route)
	maxAttempts := defaultHTTPMaxRetries + 1
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		request, err := http.NewRequestWithContext(ctx, method, url, reader)
		if err != nil {
			return nil, err
		}
		c.setHeaders(request)
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}

		response, err := client.Do(request)
		if err != nil {
			if ctx.Err() != nil || attempt == maxAttempts-1 {
				return nil, err
			}
			delay := retryDelay(nil, attempt)
			c.logf("Transient translation API error: %v; retrying in %s (%d/%d).", err, delay.Round(time.Millisecond), attempt+2, maxAttempts)
			if err := sleepContext(ctx, delay); err != nil {
				return nil, err
			}
			continue
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return response, nil
		}
		if !retryableHTTPStatus(response.StatusCode) || attempt == maxAttempts-1 {
			return response, nil
		}

		delay := retryDelay(response, attempt)
		status := response.Status
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		response.Body.Close()
		c.logf("Transient translation API response %s; retrying in %s (%d/%d).", status, delay.Round(time.Millisecond), attempt+2, maxAttempts)
		if err := sleepContext(ctx, delay); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("translation API retry loop exhausted")
}

func retryableHTTPStatus(status int) bool {
	return status == http.StatusTooManyRequests || (status >= 500 && status <= 599)
}

func retryDelay(response *http.Response, attempt int) time.Duration {
	if response != nil {
		if delay, ok := parseRetryAfter(response.Header.Get("Retry-After"), time.Now()); ok {
			return capRetryDelay(delay)
		}
	}
	delay := defaultHTTPRetryBackoff
	for i := 0; i < attempt; i++ {
		delay *= 2
	}
	return capRetryDelay(delay)
}

func capRetryDelay(delay time.Duration) time.Duration {
	if delay > maxHTTPRetryBackoff {
		return maxHTTPRetryBackoff
	}
	return delay
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0, true
		}
		return time.Duration(seconds) * time.Second, true
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	delay := when.Sub(now)
	if delay < 0 {
		delay = 0
	}
	return delay, true
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) logf(format string, args ...any) {
	if c.Log != nil {
		c.Log(fmt.Sprintf(format, args...))
	}
}

func responseError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = response.Status
	}
	return fmt.Errorf("API request failed with HTTP %d: %s", response.StatusCode, message)
}
