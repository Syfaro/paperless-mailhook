package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"strings"

	_ "github.com/joho/godotenv/autoload"
	"github.com/kelseyhightower/envconfig"

	log "github.com/sirupsen/logrus"

	"github.com/jordan-wright/email"
	"github.com/thecodingmachine/gotenberg-go-client/v7"
)

const MaxMemory = 1024 * 1024 * 10

type Config struct {
	PaperlessEndpoint string `required:"true"`
	PaperlessApiKey   string `required:"true"`
	GotenbergEndpoint string

	AllowedEmails []string `required:"true"`
	ToAddress     string

	HttpHost string `default:"127.0.0.1:5000"`
	Debug    bool
}

type SendGridEnvelope struct {
	To   []string
	From string
}

type emailHandler struct {
	cfg             Config
	client          *http.Client
	gotenbergClient *gotenberg.Client
}

func (handler *emailHandler) uploadDocument(r io.Reader, filename string) error {
	ctxLog := log.WithField("filename", filename)
	ctxLog.Info("uploading file to paperless")

	buf := &bytes.Buffer{}
	body := multipart.NewWriter(buf)

	fw, err := body.CreateFormFile("document", filename)
	if err != nil {
		return err
	}
	if _, err = io.Copy(fw, r); err != nil {
		return err
	}

	if err = body.Close(); err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/documents/post_document/", handler.cfg.PaperlessEndpoint), buf)
	if err != nil {
		return err
	}

	req.Header.Add("Authorization", fmt.Sprintf("Token %s", handler.cfg.PaperlessApiKey))
	req.Header.Add("Content-Type", body.FormDataContentType())

	resp, err := handler.client.Do(req)
	if err != nil {
		return err
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	ctxLog.Debugf("got paperless response: %s", string(data))

	return nil
}

func (handler *emailHandler) uploadAttachments(attachments []*email.Attachment) error {
	for _, file := range attachments {
		logCtx := log.WithField("filename", file.Filename)

		logCtx.Debugf("processing attachment")

		var content io.Reader
		data := bytes.NewReader(file.Content)

		switch strings.ToLower(file.Header.Get("Content-Transfer-Encoding")) {
		case "quoted-printable":
			logCtx.Trace("file was quoted-printable encoded")
			content = quotedprintable.NewReader(data)
		case "base64":
			logCtx.Trace("file was base64 encoded")
			content = base64.NewDecoder(base64.StdEncoding, data)
		default:
			logCtx.Trace("file was plain text")
			content = data
		}

		if strings.HasSuffix(strings.ToLower(file.Filename), ".eml") {
			logCtx.Infof("found eml attachment, processing")

			if err := handler.emlFile(content); err != nil {
				logCtx.Errorf("unable to process eml file: %s", err.Error())
			}

			continue
		}

		if err := handler.uploadDocument(content, file.Filename); err != nil {
			logCtx.Error("unable to upload document: %s", err.Error())

			continue
		}

		logCtx.Info("uploaded attachment")
	}

	return nil
}

func (handler *emailHandler) uploadContents(email *email.Email) error {
	var resp *http.Response = nil
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

	if err := handler.uploadDocument(resp.Body, filename); err != nil {
		log.Errorf("could not upload converted email: %s", err.Error())
	}

	return nil
}

func (handler *emailHandler) emlFile(r io.Reader) error {
	email, err := email.NewEmailFromReader(r)
	if err != nil {
		return err
	}

	if len(email.Attachments) != 0 {
		if err = handler.uploadAttachments(email.Attachments); err != nil {
			return err
		}
	} else {
		if err = handler.uploadContents(email); err != nil {
			return err
		}
	}

	return nil
}

func (handler *emailHandler) sendGrid(w http.ResponseWriter, req *http.Request) {
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

	var envelope SendGridEnvelope
	if err := json.Unmarshal([]byte(envelopeValue[0]), &envelope); err != nil {
		log.Errorf("email envelope was not expected json")

		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "bad envelope: %s", err.Error())

		return
	}

	logCtx := log.WithFields(log.Fields{
		"from": envelope.From,
		"to":   envelope.To,
	})
	logCtx.Debugf("got email")

	found := false
	for _, email := range handler.cfg.AllowedEmails {
		if strings.EqualFold(envelope.From, email) {
			found = true
			break
		}
	}

	if !found {
		logCtx.Warnf("email was from unknown sender, ignoring")

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK")

		return
	}

	if handler.cfg.ToAddress != "" {
		found := false
		for _, email := range envelope.To {
			if strings.EqualFold(email, handler.cfg.ToAddress) {
				found = true
				break
			}
		}

		if !found {
			logCtx.Warnf("email was not addressed to correct email, ignoring")

			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "OK")

			return
		}
	}

	r := strings.NewReader(req.MultipartForm.Value["email"][0])
	email, err := email.NewEmailFromReader(r)
	if err != nil {
		logCtx.Errorf("email could not be parsed: %s", err.Error())
	}

	logCtx = logCtx.WithField("subject", email.Subject)

	if len(email.Attachments) != 0 {
		logCtx.Info("got email with attachments")

		if err := handler.uploadAttachments(email.Attachments); err != nil {
			logCtx.Errorf("could not upload attachments: %s", err.Error())
		}
	} else if handler.gotenbergClient != nil {
		logCtx.Info("got email with no attachments")

		if err := handler.uploadContents(email); err != nil {
			logCtx.Errorf("could not upload email contents: %s", err.Error())
		}
	} else {
		logCtx.Warn("got unhandled email")
	}

	logCtx.Info("finished handling email")

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK")
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

	var gotenbergClient *gotenberg.Client = nil
	if cfg.GotenbergEndpoint != "" {
		log.Info("found gotenberg endpoint, enabling")
		gotenbergClient = &gotenberg.Client{Hostname: cfg.GotenbergEndpoint}
	}

	emailHandler := emailHandler{cfg, http.DefaultClient, gotenbergClient}

	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNotFound)

		fmt.Fprintf(w, "doesn't look like anything to me")
	})
	http.HandleFunc("/sendgrid", emailHandler.sendGrid)

	log.Infof("starting http server on %s", cfg.HttpHost)
	http.ListenAndServe(cfg.HttpHost, nil)
}
