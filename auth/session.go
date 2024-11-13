package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gorilla/sessions"
	"github.com/jmoiron/monet/conf"
)

const sessionJar = "monet-session"

type sessionKey struct{}

// A SessionManager manages sessions
type SessionManager struct {
	store sessions.Store
}

func NewSessionManager(cfg *conf.Config) *SessionManager {
	return &SessionManager{store: sessions.NewCookieStore([]byte(cfg.SessionSecret))}
}

// Context adds this session manager to ctx
func (s *SessionManager) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, sessionKey{}, s)
}

// Middleware adds this manager to the context, allowing any handler to utilize it
func (s *SessionManager) AddSessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(s.Context(r.Context())))
	})
}

// Session returns the session for this request
func (s *SessionManager) Session(r *http.Request) *sessions.Session {
	store, _ := s.store.Get(r, sessionJar)
	return store
}

func (s *SessionManager) RequireAuthenticatedRedirect(url string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !s.IsAuthenticated(r) {
				http.Redirect(w, r, fmt.Sprintf("%s?redirect=%s", loginUrl, url), http.StatusFound)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *SessionManager) RequireAuthenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.IsAuthenticated(r) {
			http.Redirect(w, r, loginUrl, http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *SessionManager) IsAuthenticated(r *http.Request) bool {
	store, _ := s.store.Get(r, sessionJar)
	return store.Values["authenticated"] == true
}

// SessionFromContext returns the session manager from the context
func SessionFromContext(ctx context.Context) *SessionManager {
	return ctx.Value(sessionKey{}).(*SessionManager)
}
