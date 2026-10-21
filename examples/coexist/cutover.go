package showcase

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// RouteCutover assigns one method+path operation to exactly one runtime.
// Other routes continue to use Original so unmigrated handlers stay available.
type RouteCutover struct {
	mu                  sync.RWMutex
	method, path, owner string
	original, adapted   http.Handler
}

func NewRouteCutover(app *App, method, path string) *RouteCutover {
	return &RouteCutover{method: method, path: path, owner: "original", original: app.Original, adapted: app.Build}
}

func (c *RouteCutover) SetOwner(owner string) error {
	if owner != "original" && owner != "adapted" {
		return fmt.Errorf("unknown route owner %q", owner)
	}
	c.mu.Lock()
	c.owner = owner
	c.mu.Unlock()
	return nil
}

func (c *RouteCutover) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.RLock()
	owner := c.owner
	c.mu.RUnlock()
	if r.Method == c.method && matchPath(c.path, r.URL.Path) {
		if owner == "adapted" {
			c.adapted.ServeHTTP(w, r)
		} else {
			c.original.ServeHTTP(w, r)
		}
		return
	}
	c.original.ServeHTTP(w, r)
}

func matchPath(pattern, path string) bool {
	ps, xs := strings.Split(strings.Trim(pattern, "/"), "/"), strings.Split(strings.Trim(path, "/"), "/")
	if len(ps) != len(xs) {
		return false
	}
	for i := range ps {
		if strings.HasPrefix(ps[i], "{") && strings.HasSuffix(ps[i], "}") {
			continue
		}
		if ps[i] != xs[i] {
			return false
		}
	}
	return true
}
