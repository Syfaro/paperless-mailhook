package paperless

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	Endpoint    = "http://paperless:3000"
	APIKeyValue = "apiKeyValue"

	DocumentFilename = "paperlessDocumentFilename"
	DocumentContents = "paperlessDocumentContents"
)

var (
	DocumentTags = []int{1, 2}
)

func TestNew(t *testing.T) {
	paperless := New(Endpoint, APIKeyValue, nil)

	assert.Equal(t, Endpoint, paperless.Endpoint, "new should set endpoint correctly")
	assert.Equal(t, APIKeyValue, paperless.APIKey, "new should set api key correctly")
	assert.NotNil(t, paperless.Client, "new should not leave http client as nil")
}

func TestHTTPClient(t *testing.T) {
	c := make(chan string, 1)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		c <- req.Header.Get("Authorization")
	}))
	defer ts.Close()

	client := httpClient{http.DefaultClient, APIKeyValue}
	req, _ := http.NewRequest(http.MethodGet, ts.URL, nil)
	resp, _ := client.Do(req)
	resp.Body.Close()

	var authValue string
	select {
	case auth := <-c:
		authValue = auth
	case <-time.After(1 * time.Second):
		t.Error("did not get authorization header")
	}

	assert.Equal(t, fmt.Sprintf("Token %s", APIKeyValue), authValue, "http client should set authorization header with correct api key")
}

func TestResolveTag(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		urlWithQuery := fmt.Sprintf("%s?%s", req.URL.Path, req.URL.RawQuery)
		assert.Equal(t, "/api/tags/?name__iexact=test", urlWithQuery, "resolving tags should use correct url")

		fmt.Fprint(w, `{"results": [{"id": 123}]}`)
	}))
	defer ts.Close()

	paperless := New(ts.URL, APIKeyValue, http.DefaultClient)

	tagID, err := paperless.ResolveTag("test")
	assert.Equal(t, 123, tagID, "resolving tag should have correct ID")
	assert.Nil(t, err, "should be no error resolving valid tag")

	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		urlWithQuery := fmt.Sprintf("%s?%s", req.URL.Path, req.URL.RawQuery)
		assert.Equal(t, "/api/tags/?name__iexact=test2", urlWithQuery, "resolving tags should use correct url")

		fmt.Fprint(w, `{"results": []}`)
	}))
	defer ts.Close()

	paperless = New(ts.URL, APIKeyValue, http.DefaultClient)

	tagID, err = paperless.ResolveTag("test2")
	assert.Equal(t, -1, tagID, "unresolved tag should have correct ID")
	assert.Equal(t, err, errBadTag, "unresolved tag should have expected error")
}

func TestUploadDocument(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "/api/documents/post_document/", req.URL.Path, "uploading documents should use correct url")

		err := req.ParseMultipartForm(1024 * 1024 * 10)
		require.Nil(t, err, "must not have error parsing sent multipart form")

		tags := req.MultipartForm.Value["tags"]
		require.Len(t, tags, len(DocumentTags), "document should have correct number of tags")

		var tagIDs []int
		for _, tag := range tags {
			tagID, err := strconv.Atoi(tag)
			assert.Nil(t, err, "each tag should be valid number")
			tagIDs = append(tagIDs, tagID)
		}

		assert.Equal(t, DocumentTags, tagIDs, "tag IDs should be the same")

		documents := req.MultipartForm.File["document"]
		require.Len(t, documents, 1, "only one document should be uploaded at a time")
		document := documents[0]

		assert.Equal(t, DocumentFilename, document.Filename, "document filename should be the same")

		f, err := document.Open()
		require.Nil(t, err, "should not have errors opening document file")
		data, err := io.ReadAll(f)
		require.Nil(t, err, "should be able to read document contents")

		assert.Equal(t, DocumentContents, string(data), "document should contain same data as uploaded")

		fmt.Fprint(w, "OK")
	}))
	defer ts.Close()

	paperless := New(ts.URL, APIKeyValue, http.DefaultClient)

	r := strings.NewReader(DocumentContents)
	err := paperless.UploadDocument(r, DocumentFilename, DocumentTags)
	assert.Nil(t, err, "document should upload without errors")
}
