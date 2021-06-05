# paperless-mailhook

Collect incoming emails to [Paperless-ng][paperless-ng].

Instead of relying on a dedicated email account, it uses webhooks from email
providers such as [SendGrid][sendgrid]. PRs welcome to support other providers.

## Behavior

1. Check if incoming email was from allowed email address.
2. Check if incoming email was addressed to expected email address, if enabled.
3. Check if incoming email has attachments.
    1. Check if attachment is `.eml` file.
        1. If it is, start at step 3 using contents of attached email.
        2. If not, upload to Paperless with attachment filename.
    2. If no attachments, convert email to PDF if Gotenberg is enabled, using subject as filename.

## Configuration

| Env Name                     | Description                                                             |
| ---------------------------- | ----------------------------------------------------------------------- |
| `MAILHOOK_PAPERLESSENDPOINT` | Paperless-ng endpoint, including scheme                                 |
| `MAILHOOK_PAPERLESSAPIKEY`   | Paperless-ng API key                                                    |
| `MAILHOOK_GOTENBERGENDPOINT` | Optional, [Gotenberg][gotenberg] endpoint, see behavior for more        |
| `MAILHOOK_ALLOWEDEMAILS`     | Comma separated list of email addresses allowed to upload documents     |
| `MAILHOOK_TOADDRESS`         | Optional, require incoming emails to be addressed to this email address |
| `MAILHOOK_HTTPHOST`          | Optional, host to listen for requests on, defaults to `127.0.0.1:5000`  |

### SendGrid

Currently, only SendGrid is supported for incoming email webhooks.

Inbound parse should be set to the `/sendgrid` endpoint on the domain where this
service is available. SendGrid must be set to send the raw email.

[paperless-ng]: https://github.com/jonaswinkler/paperless-ng
[sendgrid]: https://sendgrid.com/docs/ui/account-and-settings/inbound-parse/
[gotenberg]: https://github.com/thecodingmachine/gotenberg

## Docker

You can run this using Docker. An image is published at
`ghcr.io/syfaro/paperless-mailhook`.

```bash
docker run -p 5000:5000 \
    -e MAILHOOK_PAPERLESSENDPOINT=http://paperless:8000 \
    -e MAILHOOK_PAPERLESSAPIKEY=abc123 \
    -e MAILHOOK_GOTENBERGENDPOINT=http://gotenberg:3000 \
    -e MAILHOOK_ALLOWEDEMAILS=syfaro@huefox.com \
    ghcr.io/syfaro/paperless-mailhook:latest
```

### docker-compose

```yaml
services:
  paperless-mailhook:
    image: ghcr.io/syfaro/paperless-mailhook:latest
    ports:
      - '5000:5000'
    environment:
      MAILHOOK_PAPERLESSENDPOINT: http://paperless:8000
      MAILHOOK_PAPERLESSAPIKEY: abc123
      MAILHOOK_GOTENBERGENDPOINT: http://gotenberg:3000
      MAILHOOK_ALLOWEDEMAILS: syfaro@huefox.com
```
