// Package auth checks the per binding token on calls to a provider (spec 0005, "Bindings, clients
// and protection"): the clients directory holds one file per consumer, named after the consumer
// instance and holding its token; a call must carry one of them as Authorization: Bearer.
package auth

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/racherom/modular-platform/sdk/httpkit"
	"github.com/racherom/modular-platform/sdk/manifest"
)

// minTokenLen is the shortest token a clients directory may hold, after trimming.
const minTokenLen = 32

// Clients holds the consumer tokens of one provided capability.
type Clients struct {
	names  []string
	tokens [][]byte
}

// Len is the number of consumers.
func (c Clients) Len() int { return len(c.names) }

// LoadClients reads dir. Hidden entries (starting with .) are skipped; every other entry must be
// a regular file, or a symlink to one, named after a consumer instance and holding a token of at
// least 32 characters that no other file holds. It returns every broken entry; the messages name
// files, never tokens.
func LoadClients(dir string) (Clients, []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Clients{}, []error{fmt.Errorf("the clients directory %s cannot be read: %w", dir, err)}
	}
	var (
		c     Clients
		errs  []error
		owner = map[string]string{} // token to the file that holds it
	)
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := os.Stat(path) // follows a symlink, as Compose mounts can be either
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("%s cannot be read: %w", path, err))
			continue
		case !info.Mode().IsRegular():
			errs = append(errs, fmt.Errorf("%s is not a regular file", path))
			continue
		case !manifest.ValidName(name):
			errs = append(errs, fmt.Errorf("%s: the file name must be a consumer instance name", path))
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s cannot be read: %w", path, err))
			continue
		}
		token := strings.TrimRight(string(data), " \t\r\n")
		switch {
		case token == "":
			errs = append(errs, fmt.Errorf("%s is empty", path))
			continue
		case len(token) < minTokenLen:
			errs = append(errs, fmt.Errorf("%s holds a token under %d characters", path, minTokenLen))
			continue
		}
		if first, dup := owner[token]; dup {
			errs = append(errs, fmt.Errorf("%s holds the same token as %s", path, first))
			continue
		}
		owner[token] = path
		c.names = append(c.names, name)
		c.tokens = append(c.tokens, []byte(token))
	}
	return c, errs
}

type moduleKey struct{}

// CallerModule returns the consumer instance whose token a request carried.
func CallerModule(ctx context.Context) (string, bool) {
	name, ok := ctx.Value(moduleKey{}).(string)
	return name, ok
}

// Middleware admits a request only with Authorization: Bearer <token> of one of the consumers,
// compared in constant time against every token, and answers 401 otherwise. It puts the
// consumer's instance name into the request context.
func Middleware(c Clients, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		match := -1
		if ok {
			for i, token := range c.tokens {
				// every token is compared, so the time taken does not tell which one matched
				if subtle.ConstantTimeCompare([]byte(got), token) == 1 {
					match = i
				}
			}
		}
		if match < 0 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			httpkit.WriteProblem(w, http.StatusUnauthorized, "unauthorized", "This module needs a valid binding token.")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), moduleKey{}, c.names[match])))
	})
}
