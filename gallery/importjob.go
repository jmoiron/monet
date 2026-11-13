package gallery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"sync"
	"time"
)

// how long a finished import is remembered, for a page that asks after it
const jobKeep = time.Hour

// An importJob is an instagram import running (or waiting for its turn, or
// finished) in the background. It is what the page asks about while it waits.
type importJob struct {
	ID string

	mu       sync.Mutex
	state    string // queued, running, done or failed
	total    int
	done     int
	started  time.Time // when it began running
	finished time.Time
	report   *ImportReport
	err      string
}

func newImportJob(total int) *importJob {
	b := make([]byte, 8)
	rand.Read(b)
	return &importJob{ID: hex.EncodeToString(b), state: "queued", total: total}
}

func (j *importJob) begin() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.state, j.started = "running", time.Now()
}

func (j *importJob) step(done int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.done = done
}

func (j *importJob) end(rep *ImportReport, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.finished, j.report = time.Now(), rep
	if err != nil {
		j.state, j.err = "failed", err.Error()
		return
	}
	j.state, j.done = "done", j.total
}

func (j *importJob) active() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.state == "queued" || j.state == "running"
}

// status is what the page is told: where the import is, how fast it is going
// and when it should be done.
func (j *importJob) status() jsonMap {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := jsonMap{"success": true, "job": j.ID, "state": j.state, "total": j.total, "done": j.done}
	if j.state == "running" {
		if secs := time.Since(j.started).Seconds(); secs > 0 && j.done > 0 {
			rate := float64(j.done) / secs
			out["rate"] = rate
			out["eta"] = float64(j.total-j.done) / rate
		}
	}
	if j.report != nil {
		out["report"] = j.report
		out["message"] = j.report.String()
	}
	if j.err != "" {
		out["error"] = j.err
	}
	return out
}

// startImport runs the import in the background and returns the job to ask
// after it. The job owns sess and the spooled export at tmp from here on.
func (a *Admin) startImport(sess *ImportSession, tmp string) *importJob {
	j := newImportJob(sess.Media)

	a.jobsMu.Lock()
	for id, old := range a.jobs {
		if !old.active() && time.Since(old.finishedAt()) > jobKeep {
			delete(a.jobs, id)
		}
	}
	a.jobs[j.ID] = j
	a.latest = j
	a.jobsMu.Unlock()

	go func() {
		defer os.Remove(tmp)
		defer sess.Close()

		// the browser may be long gone; the import is not
		release, _ := a.acquire(context.Background())
		defer release()

		j.begin()
		started := time.Now()
		rep, err := sess.Run(context.Background(), j.step)
		j.end(rep, err)
		if err != nil {
			slog.Warn("importing instagram export", "err", err)
			return
		}
		slog.Info("imported instagram export", "report", rep.String())
		if err := recordImport(a.db, SourceInstagram, started, rep); err != nil {
			slog.Error("recording import", "err", err)
		}
	}()
	return j
}

func (j *importJob) finishedAt() time.Time {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.finished
}

func (a *Admin) job(id string) *importJob {
	a.jobsMu.Lock()
	defer a.jobsMu.Unlock()
	if id == "" {
		return a.latest
	}
	return a.jobs[id]
}
