package mtr

import "net/http"

// AddRegistryMiddleware adds reg to the request context
func AddRegistryMiddleware(reg *Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(reg.Context(r.Context())))
		})
	}
}
