package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/quotedprintable"
	"net/http"
	"strings"
	"time"

	"github.com/VictoriaMetrics/metrics"
	_ "github.com/joho/godotenv/autoload"
	"github.com/kelseyhightower/envconfig"

	log "github.com/sirupsen/logrus"

	"github.com/jordan-wright/email"
	"github.com/thecodingmachine/gotenberg-go-client/v7"

	"github.com/Syfaro/paperless-mailhook/paperless"
)

const MaxMemory = 1024 * 1024 * 10
const UserAgent = "Paperless_Mailhook/1.0 (https://github.com/Syfaro/paperless-mailhook)"

var (
	incomingEmails      = metrics.NewCounter("paperless_mailhook_incoming_emails_total")
	filteredEmails      = metrics.NewCounter("paperless_mailhook_filtered_emails_total")
	emailProcessingTime = metrics.NewHistogram("paperless_mailhook_email_processing_seconds")
)

type Config struct {
	PaperlessEndpoint string `required:"true"`
	PaperlessAPIKey   string `required:"true"`
	PaperlessTags     []string
	GotenbergEndpoint string

	AllowedEmails []string `required:"true"`
	ToAddress     string

	HTTPHost string `default:"127.0.0.1:5000"`
	Debug    bool
}

func main() {
	var cfg Config
	if err := envconfig.Process("mailhook", &cfg); err != nil {
		log.Fatal(err.Error())
	}

	if cfg.Debug {
		log.SetLevel(log.TraceLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}

	client := &http.Client{Transport: newAddHeaderTransport(nil)}

	paperless := paperless.New(cfg.PaperlessEndpoint, cfg.PaperlessAPIKey, client)

	var gotenbergClient *gotenberg.Client
	if cfg.GotenbergEndpoint != "" {
		log.Info("found gotenberg endpoint, enabling")
		gotenbergClient = &gotenberg.Client{Hostname: cfg.GotenbergEndpoint, HTTPClient: client}
	}

	tags, err := ResolveTags(paperless, cfg.PaperlessTags)
	if err != nil {
		log.Fatalf("could not resolve tags: %s", err.Error())
	}

	allowList := AllowList{cfg.AllowedEmails, cfg.ToAddress}
	emailHandler := EmailHandler{allowList, tags, paperless, gotenbergClient}

	http.HandleFunc("/sendgrid", emailHandler.sendGrid)
	http.HandleFunc("/health", func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprint(w, "OK")
	})
	http.HandleFunc("/metrics", func(w http.ResponseWriter, req *http.Request) {
		metrics.WritePrometheus(w, true)
	})

	log.Infof("starting http server on %s", cfg.HTTPHost)
	if err = http.ListenAndServe(cfg.HTTPHost, nil); err != nil {
		log.Fatalf("could not start http server: %s", err.Error())
	}
}

type EmailHandler struct {
	AllowList
	Tags []int

	paperless       *paperless.Paperless
	gotenbergClient *gotenberg.Client
}

// ProcessEmail evalulates attachments and uploads either the attachments or
// email contents to Paperless.
//
// This should only be called after ensuring an email is safe to upload.
func (handler *EmailHandler) ProcessEmail(email *email.Email) error {
	logCtx := log.WithFields(log.Fields{
		"from":    email.From,
		"to":      email.To,
		"subject": email.Subject,
	})
	logCtx.Info("processing email")

	if len(email.Attachments) == 0 {
		logCtx.Debug("email had no attachments")

		if handler.gotenbergClient == nil {
			logCtx.Warn("got email without attachments and gotenberg is disabled, skipping")
			return nil
		}

		return handler.UploadContent(email)
	}

	logCtx.Debug("email has attachments, uploading")
	for _, attachment := range email.Attachments {
		if err := handler.UploadAttachment(attachment); err != nil {
			return err
		}
	}

	return nil
}

// UploadAttachment decodes and upload an email attachment to Paperless.
func (handler *EmailHandler) UploadAttachment(attachment *email.Attachment) error {
	logCtx := log.WithField("filename", attachment.Filename)
	logCtx.Debug("processing attachment")

	// We will always need the decoded contents of the attachment.
	r := NewAttachmentReader(attachment)

	if strings.ToLower(attachment.ContentType) == "message/rfc822" {
		logCtx.Info("attachment was email, evaluating as new email")

		email, err := email.NewEmailFromReader(r)
		if err != nil {
			return err
		}

		return handler.ProcessEmail(email)
	}

	if err := handler.paperless.UploadDocument(r, attachment.Filename, handler.Tags); err != nil {
		return err
	}

	logCtx.Info("uploaded attachment")
	return nil
}

// UploadContent will convert email content to a PDF then upload to Paperless.
//
// This should only be used when Gotenberg is available.
//
// It will use the email's subject for a filename, falling back to 'Email.pdf'
// if no subject was set.
func (handler *EmailHandler) UploadContent(email *email.Email) error {
	if handler.gotenbergClient == nil {
		return errors.New("gotenberg was unavailable")
	}

	logCtx := log.WithFields(log.Fields{
		"from":    email.From,
		"to":      email.To,
		"subject": email.Subject,
	})
	logCtx.Info("converting email to pdf")

	var resp *http.Response
	if email.HTML != nil {
		index, err := gotenberg.NewDocumentFromBytes("index.html", email.HTML)
		if err != nil {
			return err
		}

		req := gotenberg.NewHTMLRequest(index)
		req.WaitTimeout(30)
		resp, err = handler.gotenbergClient.Post(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
	} else if email.Text != nil {
		index, err := gotenberg.NewDocumentFromBytes("index.txt", email.Text)
		if err != nil {
			return err
		}

		req := gotenberg.NewOfficeRequest(index)
		req.WaitTimeout(30)
		resp, err = handler.gotenbergClient.Post(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
	} else {
		return errors.New("email was empty")
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("got wrong gotenberg status code: %d", resp.StatusCode)
	}

	filename := "Email.pdf"
	if email.Subject != "" {
		filename = fmt.Sprintf("%s.pdf", email.Subject)
	}

	if err := handler.paperless.UploadDocument(resp.Body, filename, handler.Tags); err != nil {
		return err
	}

	log.Debug("uploaded email contents")
	return nil
}

// sendGridEnvelope is the envelope data included in the webhook by SendGrid.
type sendGridEnvelope struct {
	To   []string `json:"to"`
	From string   `json:"from"`
}

// sendGrid handles incoming HTTP requests from SendGrid and processes the
// associated email.
func (handler *EmailHandler) sendGrid(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	incomingEmails.Inc()

	if err := req.ParseMultipartForm(MaxMemory); err != nil {
		log.Errorf("unable to parse incoming email: %s", err.Error())

		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "bad request: %s", err.Error())

		return
	}

	envelopeValue, ok := req.MultipartForm.Value["envelope"]
	if !ok {
		log.Errorf("email was missing envelope")

		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "missing envelope")

		return
	}

	var envelope sendGridEnvelope
	if err := json.Unmarshal([]byte(envelopeValue[0]), &envelope); err != nil {
		log.Errorf("email envelope was not expected json: %s", err.Error())

		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "bad envelope: %s", err.Error())

		return
	}

	logCtx := log.WithFields(log.Fields{
		"from": envelope.From,
		"to":   envelope.To,
	})
	logCtx.Info("got email")

	if !handler.IsAllowedEmail(envelope.From, envelope.To) {
		logCtx.Warn("email was not allowed")

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK")

		return
	}

	// Email field should always be set and always have exactly one entry.
	r := strings.NewReader(req.MultipartForm.Value["email"][0])
	email, err := email.NewEmailFromReader(r)
	if err != nil {
		logCtx.Errorf("email could not be parsed: %s", err.Error())
	}

	if err = handler.ProcessEmail(email); err != nil {
		var paperlessError *paperless.PaperlessError
		if errors.As(err, &paperlessError) {
			logCtx.Errorf("could not upload document to paperless: %s", paperlessError.Error())
			logCtx.Errorf("full paperless error: %s", string(paperlessError.Body))
		} else {
			logCtx.Errorf("could not process email: %s", err.Error())
		}
	}

	logCtx.Info("finished handling email")

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK")

	emailProcessingTime.UpdateDuration(start)
}

// ResolveTags attempts to convert values of tags into their corresponding IDs.
func ResolveTags(paperless *paperless.Paperless, tags []string) ([]int, error) {
	tagIDs := make([]int, 0, len(tags))

	for _, tag := range tags {
		tagID, err := paperless.ResolveTag(tag)
		if err != nil {
			return nil, err
		}

		tagIDs = append(tagIDs, tagID)
	}

	return tagIDs, nil
}

// IsQuotedPrintable attempts to decode content to check if it is
// quotedprintable encoded.
func IsQuotedPrintable(content []byte) bool {
	r := bytes.NewReader(content)
	qp := quotedprintable.NewReader(r)

	// Read up to 1024 bytes at a time, returning as soon as an error is found.
	buf := make([]byte, 1024)
	for {
		if _, err := qp.Read(buf); err != nil {
			return errors.Is(err, io.EOF)
		}
	}
}

// IsBase64 reads the first 1024 bytes to check if content is base-64 encoded.
func IsBase64(content []byte) bool {
	buf := make([]byte, 1024)
	_, err := base64.StdEncoding.Decode(buf, content)
	return err == nil
}

// NewAttachmentReader takes an email attachment and returns a reader, decoding
// the data that it thinks is within.
func NewAttachmentReader(attachment *email.Attachment) (r io.Reader) {
	logCtx := log.WithField("filename", attachment.Filename)
	logCtx.Trace("decoding email attachment")

	// Look at header to determine how we should decode the contents of this
	// attachment.
	cte := strings.ToLower(attachment.Header.Get("Content-Transfer-Encoding"))
	logCtx.Tracef("attachment content-transfer-encoding: %s", cte)

	contentReader := bytes.NewReader(attachment.Content)

	switch cte {
	case "quoted-printable":
		if IsQuotedPrintable(attachment.Content) {
			r = quotedprintable.NewReader(contentReader)
		} else {
			logCtx.Warnf("attachment was described as %s but was not, defaulting to plain", cte)
			r = contentReader
		}
	case "base64":
		if IsBase64(attachment.Content) {
			r = base64.NewDecoder(base64.StdEncoding, contentReader)
		} else {
			logCtx.Warnf("attachment was described as %s but was not, defaulting to plain", cte)
			r = contentReader
		}
	default:
		r = contentReader
	}

	return r
}

// AllowList checks if an email was from an approved email address and
// optionally if it was addressed to a required email address.
type AllowList struct {
	AllowedEmails []string
	ToAddress     string
}

// IsAllowedEmail determines if an email is safe to process and upload.
func (allow AllowList) IsAllowedEmail(from string, to []string) bool {
	// First check if from address is in our allowlist of emails.
	found := false
	for _, email := range allow.AllowedEmails {
		if strings.EqualFold(from, email) {
			found = true
			break
		}
	}

	if !found {
		filteredEmails.Inc()
		return false
	}

	// Then check if to address is our expected address, if we're filtering
	// on that.
	if allow.ToAddress != "" {
		found := false
		for _, email := range to {
			if strings.EqualFold(email, allow.ToAddress) {
				found = true
				break
			}
		}

		if !found {
			filteredEmails.Inc()
			return false
		}
	}

	return true
}

type addHeaderTransport struct {
	T http.RoundTripper
}

func (aht *addHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Add("User-Agent", UserAgent)
	return aht.T.RoundTrip(req)
}

func newAddHeaderTransport(T http.RoundTripper) *addHeaderTransport {
	if T == nil {
		T = http.DefaultTransport
	}

	return &addHeaderTransport{T}
}
