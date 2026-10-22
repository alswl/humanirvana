package fixture

import (
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

var freeze = flag.Bool("freeze", false, "regenerate testdata from the native arm; bumps require a new ProtocolRevision")

const testdata = "../testdata"

// meta is the request side of each case; expectations are captured from the
// native Huma arm when freezing.
var meta = map[string]Case{
	"P01": {Method: http.MethodGet, Path: "/perf/params/7?count=2&enabled=true", Header: map[string]string{"X-Tenant": "fixture"}},
	"P02": {Method: http.MethodPost, Path: "/perf/json-small", Header: map[string]string{"Content-Type": "application/json"}, BodyFile: "P02.body.json"},
	"P03": {Method: http.MethodPost, Path: "/perf/json-nested", Header: map[string]string{"Content-Type": "application/json"}, BodyFile: "P03.body.json"},
	"P04": {Method: http.MethodPost, Path: "/perf/validate", Header: map[string]string{"Content-Type": "application/json"}, BodyFile: "P04.body.json"},
	"P05": {Method: http.MethodGet, Path: "/perf/middleware", Header: map[string]string{"X-Request-ID": "perf-req-1"}},
}

func TestFreezeFixtures(t *testing.T) {
	if !*freeze {
		t.Skip("run with -freeze to regenerate testdata")
	}
	native, err := NewHandler(Native)
	if err != nil {
		t.Fatal(err)
	}
	bodies := GenerateBodies()
	for _, id := range CaseIDs {
		c := meta[id]
		c.ID = id
		if c.BodyFile != "" {
			c.Body = bodies[id]
			if err := os.WriteFile(filepath.Join(testdata, c.BodyFile), c.Body, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		w := serve(t, native, c)
		c.WantStatus, c.WantContentType, c.WantBody = w.Code, w.Header().Get("Content-Type"), w.Body.String()
		data, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(testdata, id+".json"), append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteManifest(testdata); err != nil {
		t.Fatal(err)
	}
}

// The frozen files must match the generator, have the protocol byte sizes,
// and both arms must return exactly the frozen response.
func TestFrozenFixturesAreEquivalentOnBothArms(t *testing.T) {
	cases, err := Load(testdata)
	if err != nil {
		t.Fatal(err)
	}
	bodies := GenerateBodies()
	sizes := map[string]int{"P02": 1024, "P03": 16384, "P04": 1024}
	for _, c := range cases {
		if want, ok := bodies[c.ID]; ok && string(want) != string(c.Body) {
			t.Fatalf("%s: frozen body differs from generator", c.ID)
		}
		if size, ok := sizes[c.ID]; ok && len(c.Body) != size {
			t.Fatalf("%s: body is %d bytes, want %d", c.ID, len(c.Body), size)
		}
	}
	wantStatus := map[string]int{"P01": 200, "P02": 201, "P03": 200, "P04": 422, "P05": 200}
	for _, arm := range []string{Native, Adapter} {
		h, err := NewHandler(arm)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			if c.WantStatus != wantStatus[c.ID] {
				t.Fatalf("%s: frozen status %d, want %d", c.ID, c.WantStatus, wantStatus[c.ID])
			}
			w := serve(t, h, c)
			if err := c.Check(w.Code, w.Header().Get("Content-Type"), w.Body.Bytes()); err != nil {
				t.Errorf("%s arm: %v", arm, err)
			}
		}
	}
}

func serve(t *testing.T, h http.Handler, c Case) *httptest.ResponseRecorder {
	t.Helper()
	r, err := c.NewRequest("http://perf.local")
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if _, err := io.Copy(io.Discard, w.Result().Body); err != nil {
		t.Fatal(err)
	}
	return w
}
