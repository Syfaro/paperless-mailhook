package paperless

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	Endpoint    = "http://paperless:3000"
	APIKeyValue = "apiKeyValue"
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
	_, _ = client.Do(req)

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
