package handler_test

import (
	"bytes"
	"net/http"
	"net/url"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// minimalPDF is just enough to pass http.DetectContentType's %PDF- sniff.
var minimalPDF = []byte("%PDF-1.4\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF\n")

// uploadDocument POSTs raw bytes to a document endpoint (parts/projects) with a
// ?filename= and returns the raw response.
func uploadDocument(t *testing.T, ownerPath, teamID, ownerID, token, filename string, body []byte) *http.Response {
	t.Helper()
	u := testEnv.Server.URL + "/api/v1/teams/" + teamID + "/" + ownerPath + "/" + ownerID +
		"/documents?filename=" + url.QueryEscape(filename)
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload document: %v", err)
	}
	return resp
}

func TestPartDocumentLifecycle(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "doc@test.local", "pw", "Doc User")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Doc Team")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "DOC-PART-001", nil)

	// Upload a datasheet PDF.
	resp := uploadDocument(t, "parts", teamID, partID, token, "datasheet.pdf", minimalPDF)
	var up map[string]any
	decodeJSON(t, resp, &up)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201 (%v)", resp.StatusCode, up)
	}
	if up["content_type"] != "application/pdf" {
		t.Errorf("content_type = %v, want application/pdf", up["content_type"])
	}
	if up["viewable"] != true {
		t.Errorf("viewable = %v, want true for PDF", up["viewable"])
	}
	docID, _ := up["id"].(string)
	if docID == "" {
		t.Fatal("missing document id")
	}

	// List shows it.
	list, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		"/api/v1/teams/"+teamID+"/parts/"+partID+"/documents", nil, token)
	if status != http.StatusOK {
		t.Fatalf("list status = %d, want 200 (%v)", status, list)
	}
	docs, _ := list["documents"].([]any)
	if len(docs) != 1 {
		t.Fatalf("documents len = %d, want 1", len(docs))
	}
	d0, _ := docs[0].(map[string]any)
	if d0["filename"] != "datasheet.pdf" {
		t.Errorf("filename = %v, want datasheet.pdf", d0["filename"])
	}

	// Serve inline (PDF, no download flag).
	contentURL := "/api/v1/teams/" + teamID + "/documents/" + docID + "/content"
	sr := getBlob(t, contentURL, token, "")
	body := make([]byte, len(minimalPDF))
	_, _ = sr.Body.Read(body)
	sr.Body.Close()
	if sr.StatusCode != http.StatusOK {
		t.Fatalf("serve status = %d, want 200", sr.StatusCode)
	}
	if ct := sr.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("serve Content-Type = %q, want application/pdf", ct)
	}
	if cd := sr.Header.Get("Content-Disposition"); cd == "" || cd[:6] != "inline" {
		t.Errorf("serve Content-Disposition = %q, want inline...", cd)
	}

	// Explicit download forces attachment even for PDF.
	dr := getBlob(t, contentURL+"?download=1", token, "")
	dr.Body.Close()
	if cd := dr.Header.Get("Content-Disposition"); cd == "" || cd[:10] != "attachment" {
		t.Errorf("download Content-Disposition = %q, want attachment...", cd)
	}

	// Delete.
	_, status = testutil.Do(t, testEnv.Server, http.MethodDelete,
		"/api/v1/teams/"+teamID+"/documents/"+docID, nil, token)
	if status != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204", status)
	}

	// List now empty.
	list2, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		"/api/v1/teams/"+teamID+"/parts/"+partID+"/documents", nil, token)
	if docs2, _ := list2["documents"].([]any); len(docs2) != 0 {
		t.Errorf("documents after delete = %d, want 0", len(docs2))
	}
}

func TestPartDocumentImageInline(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "imgdoc@test.local", "pw", "Img Doc")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Img Doc Team")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "IMG-PART-001", nil)

	// A photo of the part is a valid document and previews inline.
	resp := uploadDocument(t, "parts", teamID, partID, token, "photo.png", makePNG(t, 6, 4))
	var up map[string]any
	decodeJSON(t, resp, &up)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("image upload status = %d, want 201 (%v)", resp.StatusCode, up)
	}
	if up["content_type"] != "image/png" {
		t.Errorf("content_type = %v, want image/png", up["content_type"])
	}
	if up["viewable"] != true {
		t.Errorf("viewable = %v, want true for image", up["viewable"])
	}
	docID, _ := up["id"].(string)

	sr := getBlob(t, "/api/v1/teams/"+teamID+"/documents/"+docID+"/content", token, "")
	sr.Body.Close()
	if ct := sr.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("serve Content-Type = %q, want image/png", ct)
	}
	if cd := sr.Header.Get("Content-Disposition"); len(cd) < 6 || cd[:6] != "inline" {
		t.Errorf("image Content-Disposition = %q, want inline...", cd)
	}
}

func TestProjectDocumentUpload(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "pdoc@test.local", "pw", "PDoc User")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "PDoc Team")
	token := testutil.CreateSession(t, testDB, userID)
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Doc Project")

	// A Touchstone file is plain ASCII; it must be served as a download.
	resp := uploadDocument(t, "projects", teamID, projectID, token, "filter.s2p",
		[]byte("! Touchstone\n# HZ S RI R 50\n1.0 0.1 0.2 0.3 0.4 0.5 0.6 0.7 0.8\n"))
	var up map[string]any
	decodeJSON(t, resp, &up)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201 (%v)", resp.StatusCode, up)
	}
	if up["viewable"] != false {
		t.Errorf("viewable = %v, want false for .s2p", up["viewable"])
	}
	docID, _ := up["id"].(string)

	// Even without download=1, a non-PDF is forced to attachment disposition.
	sr := getBlob(t, "/api/v1/teams/"+teamID+"/documents/"+docID+"/content", token, "")
	sr.Body.Close()
	if cd := sr.Header.Get("Content-Disposition"); cd == "" || cd[:10] != "attachment" {
		t.Errorf("non-PDF Content-Disposition = %q, want attachment...", cd)
	}
}

func TestDocumentRejectsUnsupportedExtension(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "doc2@test.local", "pw", "Doc User2")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Doc Team2")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "DOC-PART-002", nil)

	resp := uploadDocument(t, "parts", teamID, partID, token, "malware.exe", []byte("MZ\x90\x00"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want 415 for .exe", resp.StatusCode)
	}
}

func TestDocumentRejectsMislabeledPDF(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "doc3@test.local", "pw", "Doc User3")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Doc Team3")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "DOC-PART-003", nil)

	// An HTML payload named .pdf must be rejected — PDFs are the only inline-served
	// type, so they are magic-byte verified.
	resp := uploadDocument(t, "parts", teamID, partID, token, "evil.pdf",
		[]byte("<html><script>alert(1)</script></html>"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want 415 for non-PDF bytes named .pdf", resp.StatusCode)
	}
}

func TestDocumentUploadRequiresMember(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "doc4@test.local", "pw", "Doc User4")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "Doc Team4")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "DOC-PART-004", nil)

	// Viewer can list.
	if _, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		"/api/v1/teams/"+teamID+"/parts/"+partID+"/documents", nil, token); status != http.StatusOK {
		t.Fatalf("viewer list status = %d, want 200", status)
	}
	// But not upload.
	resp := uploadDocument(t, "parts", teamID, partID, token, "x.pdf", minimalPDF)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer upload status = %d, want 403", resp.StatusCode)
	}
}

func TestDocumentCrossTeamIsolation(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	// Team A uploads a document on a global part.
	userA := testutil.CreateUser(t, testDB, "doca@test.local", "pw", "Doc A")
	teamA := testutil.CreateTeam(t, testDB, userA, "member", "Doc Team A")
	tokenA := testutil.CreateSession(t, testDB, userA)
	partID := testutil.CreatePart(t, testDB, "global", "DOC-SHARED", nil)

	resp := uploadDocument(t, "parts", teamA, partID, tokenA, "a.pdf", minimalPDF)
	var up map[string]any
	decodeJSON(t, resp, &up)
	docID, _ := up["id"].(string)

	// Team B (different team, same global part) must not resolve team A's document.
	userB := testutil.CreateUser(t, testDB, "docb@test.local", "pw", "Doc B")
	teamB := testutil.CreateTeam(t, testDB, userB, "member", "Doc Team B")
	tokenB := testutil.CreateSession(t, testDB, userB)

	// Team B's listing of the same global part is empty (its own documents only).
	listB, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		"/api/v1/teams/"+teamB+"/parts/"+partID+"/documents", nil, tokenB)
	if docs, _ := listB["documents"].([]any); len(docs) != 0 {
		t.Errorf("team B sees %d documents on shared part, want 0", len(docs))
	}

	// And team B cannot serve team A's document by id → 404.
	sr := getBlob(t, "/api/v1/teams/"+teamB+"/documents/"+docID+"/content", tokenB, "")
	sr.Body.Close()
	if sr.StatusCode != http.StatusNotFound {
		t.Errorf("cross-team serve status = %d, want 404", sr.StatusCode)
	}
}
