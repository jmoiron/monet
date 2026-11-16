package classify

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultPrompt asks for what search needs: a sentence, and short tags that
// name things and not impressions.
const DefaultPrompt = `Describe this photograph in one or two plain sentences. Then list 10 to 20 short lowercase tags ` +
	`for what is in it: objects, people, animals, food, plants, buildings, scenery, setting, time of day, ` +
	`weather, colours, and any text that can be read. Name what you see; do not guess at what you can't.`

// OllamaVLM asks a vision language model that ollama is serving for a caption
// and tags. It needs only ollama's HTTP API, so any model it can run (qwen-vl,
// gemma, moondream, minicpm-v, llava...) is a different Model string.
type OllamaVLM struct {
	Host   string // default http://localhost:11434
	Model  string
	Prompt string // default DefaultPrompt
	// MaxEdge is the longest side the image is sent at. The cost of a
	// picture is in its size, so this is the speed knob. Default 896.
	MaxEdge int
	Client  *http.Client
	// KeepAlive keeps the model loaded between photos (an ollama duration,
	// eg. "10m"). Default is ollama's own.
	KeepAlive string
}

func (o *OllamaVLM) Name() string { return "ollama:" + o.Model }
func (o *OllamaVLM) Close() error { return nil }

func (o *OllamaVLM) host() string {
	if o.Host == "" {
		return "http://localhost:11434"
	}
	return strings.TrimRight(o.Host, "/")
}

var ollamaSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"caption": map[string]any{"type": "string"},
		"tags":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	},
	"required": []string{"caption", "tags"},
}

func (o *OllamaVLM) Classify(ctx context.Context, path string) (*Result, error) {
	edge := o.MaxEdge
	if edge == 0 {
		edge = 896
	}
	data, err := Prepare(path, edge)
	if err != nil {
		return nil, err
	}
	prompt := o.Prompt
	if prompt == "" {
		prompt = DefaultPrompt
	}
	req := map[string]any{
		"model":  o.Model,
		"stream": false,
		// the answer is constrained to the schema, which small models need
		"format": ollamaSchema,
		// reasoning models would otherwise spend their answer thinking, and
		// be cut off before the JSON
		"think":   false,
		"options": map[string]any{"temperature": 0},
		"messages": []map[string]any{{
			"role": "user", "content": prompt,
			"images": []string{base64.StdEncoding.EncodeToString(data)},
		}},
	}
	if o.KeepAlive != "" {
		req["keep_alive"] = o.KeepAlive
	}
	body, _ := json.Marshal(req)
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.host()+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	resp, err := client.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("ollama: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama: %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("ollama: %w", err)
	}
	return parseTagged(out.Message.Content)
}

// parseTagged reads the {"caption", "tags"} a model was asked for.
func parseTagged(content string) (*Result, error) {
	var a struct {
		Caption string   `json:"caption"`
		Tags    []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(content), &a); err != nil {
		return nil, fmt.Errorf("the model's answer is not the json asked for (%v): %.200q", err, content)
	}
	res := &Result{Caption: strings.TrimSpace(a.Caption)}
	seen := map[string]bool{}
	for _, t := range a.Tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			res.Labels = append(res.Labels, Label{Name: t})
		}
	}
	return res, nil
}
