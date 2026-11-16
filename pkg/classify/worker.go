package classify

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// A Worker is a program that stays running and answers questions about
// images, one json line each way over its stdin and stdout. That is the way
// to try a model that has no Go runtime worth using (open_clip, SigLIP 2,
// RAM++, an ONNX model in python...): a worker in a few dozen lines of python
// loads the model once and then answers in milliseconds. See
// scripts/worker_openclip.py.
//
// A request is {"op":"image","path":"/abs/photo.jpg"} or
// {"op":"text","text":"pizza"}. The answer is a Result, plus an "error"
// string when it failed:
//
//	{"labels":[{"name":"pizza","score":0.93}], "caption":"...", "embedding":[...]}
//
// The worker's stderr goes to this process's, for its logs and progress.
type Worker struct {
	name string

	mu    sync.Mutex
	cmd   *exec.Cmd
	in    io.WriteCloser
	out   *bufio.Reader
	ready bool
}

type workerRequest struct {
	Op   string `json:"op"`
	Path string `json:"path,omitempty"`
	Text string `json:"text,omitempty"`
}

type workerResponse struct {
	Result
	Error string `json:"error,omitempty"`
}

// StartWorker runs argv as a worker.
func StartWorker(name string, argv ...string) (*Worker, error) {
	if len(argv) == 0 {
		return nil, errors.New("a worker needs a command")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stderr = stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", argv[0], err)
	}
	return &Worker{name: name, cmd: cmd, in: in, out: bufio.NewReaderSize(out, 1<<20)}, nil
}

func (w *Worker) Name() string { return w.name }

func (w *Worker) ask(ctx context.Context, req workerRequest) (*Result, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	line, _ := json.Marshal(req)
	if _, err := w.in.Write(append(line, '\n')); err != nil {
		return nil, fmt.Errorf("worker %s: %w", w.name, err)
	}
	// a worker that is stuck can't be interrupted without losing its place in
	// the conversation, so on cancel it is killed
	type reply struct {
		line []byte
		err  error
	}
	ch := make(chan reply, 1)
	go func() {
		b, err := w.out.ReadBytes('\n')
		ch <- reply{b, err}
	}()
	select {
	case <-ctx.Done():
		w.cmd.Process.Kill()
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil && len(bytes.TrimSpace(r.line)) == 0 {
			return nil, fmt.Errorf("worker %s: %w", w.name, r.err)
		}
		var resp workerResponse
		if err := json.Unmarshal(r.line, &resp); err != nil {
			return nil, fmt.Errorf("worker %s: bad answer: %w", w.name, err)
		}
		if resp.Error != "" {
			return nil, errors.New(resp.Error)
		}
		return &resp.Result, nil
	}
}

func (w *Worker) Classify(ctx context.Context, path string) (*Result, error) {
	return w.ask(ctx, workerRequest{Op: "image", Path: path})
}

// EmbedText makes a Worker a TextEmbedder, if the worker answers "text".
func (w *Worker) EmbedText(ctx context.Context, text string) ([]float32, error) {
	res, err := w.ask(ctx, workerRequest{Op: "text", Text: text})
	if err != nil {
		return nil, err
	}
	if len(res.Embedding) == 0 {
		return nil, errors.New("the worker gave no embedding for the text")
	}
	return res.Embedding, nil
}

func (w *Worker) Close() error {
	w.in.Close()
	return w.cmd.Wait()
}
