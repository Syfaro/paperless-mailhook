FROM golang:1.16-alpine AS builder
WORKDIR /go/src/app
COPY . .
RUN go build -v ./...

FROM alpine
EXPOSE 5000
ENV MAILHOOK_HTTPHOST=0.0.0.0:5000
COPY --from=builder /go/src/app/paperless-mailhook /paperless-mailhook
CMD [ "/paperless-mailhook" ]
