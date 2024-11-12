package app

import (
	"log/slog"
	"net/http"
)

// http helpers

func Http500(msg string, w http.ResponseWriter, err error) {
	slog.Error(msg, "err", err)
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

func Http404(w http.ResponseWriter) {
	http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
}
