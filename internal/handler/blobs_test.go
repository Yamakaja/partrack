package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// decodeJSON reads and decodes a response body into v, then closes it.
func decodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, v); err != nil {
			t.Fatalf("decode json: %v (body: %s)", err, data)
		}
	}
}

// makePNG returns the encoded bytes of a w×h PNG.
func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// uploadAttachment POSTs raw bytes to the attachments endpoint and returns the
// raw response (the body is plain bytes, so testutil.Do's JSON path is unsuitable).
func uploadAttachment(t *testing.T, teamID, token string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost,
		testEnv.Server.URL+"/api/v1/teams/"+teamID+"/attachments", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	return resp
}

func getBlob(t *testing.T, url, token, ifNoneMatch string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, testEnv.Server.URL+url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get blob: %v", err)
	}
	return resp
}

func TestUploadAndServeAttachment(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "blob@test.local", "pw", "Blob User")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Blob Team")
	token := testutil.CreateSession(t, testDB, userID)

	pngBytes := makePNG(t, 2, 3)

	// Upload.
	resp := uploadAttachment(t, teamID, token, pngBytes)
	var up map[string]any
	decodeJSON(t, resp, &up)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201 (%v)", resp.StatusCode, up)
	}
	if up["content_type"] != "image/png" {
		t.Errorf("content_type = %v, want image/png", up["content_type"])
	}
	if up["width"] != float64(2) || up["height"] != float64(3) {
		t.Errorf("dimensions = %vx%v, want 2x3", up["width"], up["height"])
	}
	url, _ := up["url"].(string)
	if !strings.HasPrefix(url, "/api/v1/blobs/") {
		t.Fatalf("url = %q, want /api/v1/blobs/ prefix", url)
	}
	hash := strings.TrimPrefix(url, "/api/v1/blobs/")

	// Serve.
	br := getBlob(t, url, token, "")
	gotBytes, _ := io.ReadAll(br.Body)
	br.Body.Close()
	if br.StatusCode != http.StatusOK {
		t.Fatalf("serve status = %d, want 200", br.StatusCode)
	}
	if !bytes.Equal(gotBytes, pngBytes) {
		t.Errorf("served bytes differ from uploaded (%d vs %d)", len(gotBytes), len(pngBytes))
	}
	if cc := br.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q, want immutable", cc)
	}
	etag := br.Header.Get("ETag")
	if etag != `"`+hash+`"` {
		t.Errorf("ETag = %q, want %q", etag, `"`+hash+`"`)
	}

	// Conditional GET → 304.
	nm := getBlob(t, url, token, etag)
	nm.Body.Close()
	if nm.StatusCode != http.StatusNotModified {
		t.Errorf("conditional GET status = %d, want 304", nm.StatusCode)
	}

	// Re-upload identical bytes: a second attachment, but the same single blob (dedup).
	resp2 := uploadAttachment(t, teamID, token, pngBytes)
	var up2 map[string]any
	decodeJSON(t, resp2, &up2)
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("re-upload status = %d, want 201", resp2.StatusCode)
	}
	if up2["id"] == up["id"] {
		t.Errorf("expected a distinct attachment id on re-upload")
	}
	if up2["url"] != up["url"] {
		t.Errorf("expected identical blob url on re-upload (dedup): %v vs %v", up2["url"], up["url"])
	}

	var blobCount, attachCount int
	ctx := context.Background()
	if err := testDB.QueryRow(ctx, "SELECT count(*) FROM blobs WHERE sha256 = $1", hash).Scan(&blobCount); err != nil {
		t.Fatalf("count blobs: %v", err)
	}
	if err := testDB.QueryRow(ctx, "SELECT count(*) FROM attachments WHERE blob_sha256 = $1", hash).Scan(&attachCount); err != nil {
		t.Fatalf("count attachments: %v", err)
	}
	if blobCount != 1 {
		t.Errorf("blob rows = %d, want 1 (dedup)", blobCount)
	}
	if attachCount != 2 {
		t.Errorf("attachment rows = %d, want 2", attachCount)
	}
}

func TestUploadAttachmentRejectsUnsupportedType(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "blob2@test.local", "pw", "Blob User2")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Blob Team2")
	token := testutil.CreateSession(t, testDB, userID)

	resp := uploadAttachment(t, teamID, token, []byte("this is plain text, not an image"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", resp.StatusCode)
	}
}
