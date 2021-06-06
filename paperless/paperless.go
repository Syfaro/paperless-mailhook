// Package paperless contains code for talking to a Paperless-ng instance.
package paperless

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"

	log "github.com/sirupsen/logrus"
)

var (
	errBadTag = errors.New("got incorrect number of tags")
)

// Paperless represents a connection to a Paperless-ng instance.
type Paperless struct {
	Endpoint string
	APIKey   string

	Client HTTPClient
}

type PaperlessError struct {
	Message string
	Body    []byte
}

func (err PaperlessError) Error() string {
	return err.Message
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// New creates a new Paperless instance for a given endpoint and API key.
func New(endpoint string, apiKey string, client HTTPClient) *Paperless {
	if client == nil {
		client = http.DefaultClient
	}

	return &Paperless{
		Endpoint: endpoint,
		APIKey:   apiKey,

		Client: &httpClient{client, apiKey},
	}
}

type httpClient struct {
	c HTTPClient

	apiKey string
}

func (c *httpClient) Do(req *http.Request) (*http.Response, error) {
	req.Header.Add("Authorization", fmt.Sprintf("Token %s", c.apiKey))
	return c.c.Do(req)
}

// UploadDocument uploads a document to the given Paperless instance with the
// provided filename and tag IDs.
func (paperless *Paperless) UploadDocument(r io.Reader, filename string, tags []int) error {
	logCtx := log.WithField("filename", filename)
	logCtx.Debug("uploading file to paperless")

	buf := &bytes.Buffer{}
	body := multipart.NewWriter(buf)

	fw, err := body.CreateFormFile("document", filename)
	if err != nil {
		return err
	}
	if _, err = io.Copy(fw, r); err != nil {
		return err
	}

	for _, tag := range tags {
		if err = body.WriteField("tags", fmt.Sprint(tag)); err != nil {
			return err
		}
	}

	if err = body.Close(); err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/documents/post_document/", paperless.Endpoint), buf)
	if err != nil {
		return err
	}

	req.Header.Add("Content-Type", body.FormDataContentType())

	resp, err := paperless.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			logCtx.Errorf("could not read paperless error response")
		}

		return &PaperlessError{
			Message: fmt.Sprintf("got bad paperless status code: %d", resp.StatusCode),
			Body:    body,
		}
	}

	return nil
}

type tagResults struct {
	Results []struct {
		ID int `json:"id"`
	} `json:"results"`
}

// ResolveTag attempts to resolve a tag name into a Paperless tag ID.
func (paperless *Paperless) ResolveTag(tag string) (int, error) {
	logCtx := log.WithField("tag", tag)
	logCtx.Debug("looking up tag")

	endpoint := fmt.Sprintf("%s/api/tags/?name__iexact=%s", paperless.Endpoint, url.QueryEscape(tag))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return -1, err
	}

	resp, err := paperless.Client.Do(req)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()

	var results tagResults
	decoder := json.NewDecoder(resp.Body)
	if err = decoder.Decode(&results); err != nil {
		return -1, err
	}

	if len(results.Results) != 1 {
		return -1, errBadTag
	}

	logCtx.Tracef("resolved tag to ID %d", results.Results[0].ID)

	return results.Results[0].ID, nil
}
