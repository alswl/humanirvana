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
	mu                sync.RWMutex
	owners            map[routeKey]string
	original, adapted http.Handler
}

func NewRouteCutover(app *App, method, path string) *RouteCutover {
	c, err := NewRouteCutoverFor(app, map[string]string{method + " " + path: "original"})
	if err != nil {
		panic(err)
	}
	return c
}

type routeKey struct{ method, path string }

// NewRouteCutoverFor validates the entire application ownership map before
// publishing a selector. Keys use "METHOD /path/{param}" and each key has one owner.
func NewRouteCutoverFor(app *App, routes map[string]string) (*RouteCutover, error) {
	if app == nil {
		return nil, fmt.Errorf("nil showcase app")
	}
	owners := make(map[routeKey]string, len(routes))
	for declaration, owner := range routes {
		fields := strings.Fields(declaration)
		if len(fields) != 2 || !strings.HasPrefix(fields[1], "/") {
			return nil, fmt.Errorf("invalid route ownership %q", declaration)
		}
		key := routeKey{method: fields[0], path: fields[1]}
		if _, exists := owners[key]; exists {
			return nil, fmt.Errorf("duplicate route ownership %s %s", key.method, key.path)
		}
		if owner != "original" && owner != "adapted" {
			return nil, fmt.Errorf("unknown route owner %q", owner)
		}
		owners[key] = owner
	}
	for key := range owners {
		for other := range owners {
			if key == other || key.method != other.method {
				continue
			}
			if pathPatternsOverlap(key.path, other.path) {
				return nil, fmt.Errorf("overlapping route ownership %s %s and %s %s", key.method, key.path, other.method, other.path)
			}
		}
	}
	return &RouteCutover{owners: owners, original: app.Original, adapted: app.Build}, nil
}

func (c *RouteCutover) SetOwner(owner string) error {
	if owner != "original" && owner != "adapted" {
		return fmt.Errorf("unknown route owner %q", owner)
	}
	c.mu.Lock()
	for key := range c.owners {
		c.owners[key] = owner
	}
	c.mu.Unlock()
	return nil
}

func (c *RouteCutover) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.RLock()
	owner := ""
	for key, candidate := range c.owners {
		if r.Method == key.method && matchPath(key.path, r.URL.Path) {
			owner = candidate
			break
		}
	}
	c.mu.RUnlock()
	if owner != "" {
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

func pathPatternsOverlap(a, b string) bool {
	left, right := strings.Split(strings.Trim(a, "/"), "/"), strings.Split(strings.Trim(b, "/"), "/")
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		leftParam := strings.HasPrefix(left[i], "{") && strings.HasSuffix(left[i], "}")
		rightParam := strings.HasPrefix(right[i], "{") && strings.HasSuffix(right[i], "}")
		if !leftParam && !rightParam && left[i] != right[i] {
			return false
		}
	}
	return true
}
