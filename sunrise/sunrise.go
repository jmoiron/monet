package sunrise

import (
	"github.com/go-chi/chi/v5"
)

// Sunrise is a collection of interfaces that allow monet to put together a
// website from independently implemented modules.

// An App is a contained implementation of a portion of a website.  An
// example of an App is a blog, a wiki, an image gallery, etc.  Monet is
// essentially a collection of apps attached onto different paths.
type App interface {
	Attach(r chi.Router, path string) error
	Migrate() error
	Admin() (Admin, error)
}

// An Admin provides administration for an App;  generally CRUD.
// The panel that an App's Admin renders is used in the Admin home.
type Admin interface {
	Attach(r chi.Router, path string) error
	Panel() ([]byte, error)
}
