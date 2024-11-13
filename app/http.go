package app

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// http helpers

func Http500(msg string, w http.ResponseWriter, err error) {
	slog.Error(msg, "err", err)
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

func Http404(w http.ResponseWriter) {
	http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
}

func GetIntParam(r *http.Request, name string, _default int) int {
	x := _default
	if s := chi.URLParam(r, name); len(s) > 0 {
		x, _ = strconv.Atoi(s)
	}
	return x
}
