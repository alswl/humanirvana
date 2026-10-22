package fixture

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Case is one frozen PERF-01 request set: the exact request and the response
// every sample must return.
type Case struct {
	ID              string            `json:"id"`
	Method          string            `json:"method"`
	Path            string            `json:"path"`
	Header          map[string]string `json:"header"`
	BodyFile        string            `json:"body_file,omitempty"`
	WantStatus      int               `json:"want_status"`
	WantContentType string            `json:"want_content_type"`
	WantBody        string            `json:"want_body"`

	Body []byte `json:"-"`
}

// CaseIDs lists the request sets in protocol order.
var CaseIDs = []string{"P01", "P02", "P03", "P04", "P05"}

// GenerateBodies builds the request bodies with their exact byte lengths.
func GenerateBodies() map[string][]byte {
	small := func(quantity int) []byte {
		return padJSON(fmt.Sprintf(`{"quantity":%d,"padding":"`, quantity), `"}`, 1024)
	}
	var items strings.Builder
	colors := []string{"red", "green", "blue"}
	for i := 0; i < 32; i++ {
		if i > 0 {
			items.WriteByte(',')
		}
		fmt.Fprintf(&items, `{"id":%d,"name":"item-%02d","attrs":{"color":"%s","size":%d}}`, i, i, colors[i%3], i*10)
	}
	return map[string][]byte{
		"P02": small(1),
		"P03": padJSON(`{"items":[`+items.String()+`],"padding":"`, `"}`, 16384),
		"P04": small(0),
	}
}

func padJSON(prefix, suffix string, size int) []byte {
	n := size - len(prefix) - len(suffix)
	if n < 0 {
		panic("fixture body exceeds target size")
	}
	return []byte(prefix + strings.Repeat("x", n) + suffix)
}

// Load reads the frozen cases from dir and verifies every file against
// MANIFEST.sha256.
func Load(dir string) ([]Case, error) {
	manifest, err := readManifest(filepath.Join(dir, "MANIFEST.sha256"))
	if err != nil {
		return nil, err
	}
	for name, want := range manifest {
		got, err := fileSHA256(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if got != want {
			return nil, fmt.Errorf("fixture %s changed: sha256 %s, manifest %s", name, got, want)
		}
	}
	cases := make([]Case, 0, len(CaseIDs))
	for _, id := range CaseIDs {
		name := id + ".json"
		if _, ok := manifest[name]; !ok {
			return nil, fmt.Errorf("fixture %s missing from manifest", name)
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		var c Case
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("fixture %s: %w", name, err)
		}
		if c.BodyFile != "" {
			if _, ok := manifest[c.BodyFile]; !ok {
				return nil, fmt.Errorf("fixture %s missing from manifest", c.BodyFile)
			}
			if c.Body, err = os.ReadFile(filepath.Join(dir, c.BodyFile)); err != nil {
				return nil, err
			}
		}
		cases = append(cases, c)
	}
	return cases, nil
}

// NewRequest builds the frozen request against base, e.g. "http://127.0.0.1:1".
func (c Case) NewRequest(base string) (*http.Request, error) {
	var body io.Reader
	if c.Body != nil {
		body = bytes.NewReader(c.Body)
	}
	r, err := http.NewRequest(c.Method, base+c.Path, body)
	if err != nil {
		return nil, err
	}
	for k, v := range c.Header {
		r.Header.Set(k, v)
	}
	return r, nil
}

// Check compares a response with the frozen expectation.
func (c Case) Check(status int, contentType string, body []byte) error {
	if status != c.WantStatus {
		return fmt.Errorf("%s: status %d, want %d", c.ID, status, c.WantStatus)
	}
	if contentType != c.WantContentType {
		return fmt.Errorf("%s: Content-Type %q, want %q", c.ID, contentType, c.WantContentType)
	}
	if string(body) != c.WantBody {
		return fmt.Errorf("%s: body %q, want %q", c.ID, body, c.WantBody)
	}
	return nil
}

func readManifest(name string) (map[string]string, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		out[fields[1]] = fields[0]
	}
	return out, scanner.Err()
}

func fileSHA256(name string) (string, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// WriteManifest records the sha256 of every regular file in dir except the
// manifest itself.
func WriteManifest(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && e.Name() != "MANIFEST.sha256" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		sum, err := fileSHA256(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s  %s\n", sum, name)
	}
	return os.WriteFile(filepath.Join(dir, "MANIFEST.sha256"), []byte(b.String()), 0o644)
}
