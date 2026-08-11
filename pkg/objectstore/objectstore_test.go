package objectstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
	"github.com/Helvethink/infrahub-go-sdk/pkg/objectstore"
	"github.com/Helvethink/infrahub-go-sdk/pkg/tracking"
)

func newService(t *testing.T, handler http.HandlerFunc) (*objectstore.Service, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	client, err := api.NewClient(server.URL+"/base", api.Config{
		HTTPClient: server.Client(), DefaultBranch: "main", UserAgent: "test",
	})
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	return objectstore.NewService(client), server
}

func TestGetEscapesIdentifierAndUsesContextTracker(t *testing.T) {
	t.Parallel()
	service, server := newService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/base/api/storage/object/folder%2Fobject%20one" {
			t.Errorf("path = %q", r.URL.EscapedPath())
		}
		if tracker := r.Header.Get("X-Infrahub-Tracker"); tracker != "workflow-object" {
			t.Errorf("tracker = %q", tracker)
		}
		_, _ = w.Write([]byte("stored content"))
	})
	defer server.Close()
	content, err := service.Get(tracking.WithTracker(context.Background(), "workflow-object"), "folder/object one")
	if err != nil || content != "stored content" {
		t.Fatalf("Get() = %q, %v", content, err)
	}
}

func TestGetEscapesDotSegments(t *testing.T) {
	t.Parallel()
	paths := make(chan string, 2)
	service, server := newService(t, func(w http.ResponseWriter, r *http.Request) {
		paths <- r.URL.EscapedPath()
		_, _ = w.Write([]byte("content"))
	})
	defer server.Close()
	for _, identifier := range []string{".", ".."} {
		if _, err := service.Get(context.Background(), identifier); err != nil {
			t.Fatal(err)
		}
	}
	if got := <-paths; got != "/base/api/storage/object/%2E" {
		t.Fatalf("dot path = %q", got)
	}
	if got := <-paths; got != "/base/api/storage/object/%2E%2E" {
		t.Fatalf("dot-dot path = %q", got)
	}
}

func TestUploadSendsJSONAndDecodesTypedResult(t *testing.T) {
	t.Parallel()
	service, server := newService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/base/api/storage/upload/content" {
			t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Errorf("content type = %q", contentType)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["content"] != "hello\nworld" {
			t.Errorf("payload = %#v", payload)
		}
		_, _ = w.Write([]byte(`{"identifier":"storage-id","checksum":"abc123"}`))
	})
	defer server.Close()
	result, err := service.Upload(context.Background(), "hello\nworld")
	if err != nil || result.Identifier != "storage-id" || result.Checksum != "abc123" {
		t.Fatalf("Upload() = %#v, %v", result, err)
	}
}

func TestFileMethodsValidateTextAndEncodeHFID(t *testing.T) {
	t.Parallel()
	requests := make(chan *http.Request, 3)
	service, server := newService(t, func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		_, _ = w.Write([]byte("name: infrahub"))
	})
	defer server.Close()
	methods := []func() (string, error){
		func() (string, error) { return service.GetFileByStorageID(context.Background(), "storage/a") },
		func() (string, error) { return service.GetFileByID(context.Background(), "node/a") },
		func() (string, error) {
			return service.GetFileByHFID(context.Background(), "CoreFile/object", []string{"one two", "a/b"})
		},
	}
	for _, method := range methods {
		content, err := method()
		if err != nil || content != "name: infrahub" {
			t.Fatalf("file method = %q, %v", content, err)
		}
	}
	first, second, third := <-requests, <-requests, <-requests
	if first.URL.EscapedPath() != "/base/api/storage/files/by-storage-id/storage%2Fa" {
		t.Errorf("storage path = %q", first.URL.EscapedPath())
	}
	if second.URL.EscapedPath() != "/base/api/storage/files/node%2Fa" {
		t.Errorf("node path = %q", second.URL.EscapedPath())
	}
	if third.URL.EscapedPath() != "/base/api/storage/files/by-hfid/CoreFile%2Fobject" ||
		!reflect.DeepEqual(third.URL.Query()["hfid"], []string{"one two", "a/b"}) {
		t.Errorf("HFID URL = %s", third.URL.String())
	}
}

func TestFileRejectsBinaryContentWithTypedError(t *testing.T) {
	t.Parallel()
	service, server := newService(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0, 1, 2})
	})
	defer server.Close()
	_, err := service.GetFileByID(context.Background(), "binary-id")
	var contentTypeError *objectstore.UnsupportedContentTypeError
	if !errors.As(err, &contentTypeError) || contentTypeError.Identifier != "binary-id" {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestHTTPAndDecodeErrorsArePreserved(t *testing.T) {
	t.Parallel()
	t.Run("HTTP", func(t *testing.T) {
		service, server := newService(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
		defer server.Close()
		_, err := service.Get(context.Background(), "missing")
		var httpError *api.HTTPError
		if !errors.As(err, &httpError) || httpError.StatusCode != http.StatusNotFound {
			t.Fatalf("error = %T %v", err, err)
		}
	})
	t.Run("malformed upload", func(t *testing.T) {
		service, server := newService(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{")) })
		defer server.Close()
		if _, err := service.Upload(context.Background(), "content"); err == nil {
			t.Fatal("Upload() error = nil")
		}
	})
}

func TestRequiredIdentifiers(t *testing.T) {
	t.Parallel()
	service := objectstore.NewService(nil)
	checks := []func() error{
		func() error { _, err := service.Get(context.Background(), ""); return err },
		func() error { _, err := service.GetFileByStorageID(context.Background(), ""); return err },
		func() error { _, err := service.GetFileByID(context.Background(), ""); return err },
		func() error { _, err := service.GetFileByHFID(context.Background(), "", []string{"x"}); return err },
		func() error { _, err := service.GetFileByHFID(context.Background(), "Kind", nil); return err },
	}
	for _, check := range checks {
		if err := check(); err == nil {
			t.Fatal("validation error = nil")
		}
	}
}
