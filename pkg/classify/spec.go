package classify

import (
	"fmt"
	"regexp"
	"strings"
)

// A Spec names a backend and its settings in a string, so that one can be
// given on a command line and its results kept in a file of the same name:
//
//	ollama:qwen2.5vl:7b             a model in ollama (at localhost)
//	ollama:gemma3:4b@box:11434      ...or an ollama on another machine
//	worker:python3 scripts/worker_openclip.py --model ViT-B-16-SigLIP2
//
// A spec can be prefixed with a name and "=", which is then what the results
// file is called: "siglip2=worker:python3 ...".
type Spec string

var unsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// Name is what the spec's results are called.
func (s Spec) Name() string {
	if name, rest, ok := strings.Cut(string(s), "="); ok && !strings.ContainsAny(name, ": ") {
		_ = rest
		return unsafe.ReplaceAllString(name, "_")
	}
	n := strings.Trim(unsafe.ReplaceAllString(string(s), "_"), "_")
	if len(n) > 80 {
		n = n[:80]
	}
	return n
}

// Open makes the backend the spec describes.
func (s Spec) Open() (Classifier, error) {
	spec := string(s)
	if name, rest, ok := strings.Cut(spec, "="); ok && !strings.ContainsAny(name, ": ") {
		spec = rest
	}
	kind, rest, _ := strings.Cut(spec, ":")
	switch kind {
	case "ollama":
		model, host, _ := strings.Cut(rest, "@")
		if model == "" {
			return nil, fmt.Errorf("%q: no model", s)
		}
		o := &OllamaVLM{Model: model}
		if host != "" {
			if !strings.Contains(host, "://") {
				host = "http://" + host
			}
			o.Host = host
		}
		return o, nil
	case "worker":
		argv := strings.Fields(rest)
		return StartWorker(s.Name(), argv...)
	}
	return nil, fmt.Errorf("%q: unknown backend (ollama:MODEL or worker:COMMAND)", s)
}
