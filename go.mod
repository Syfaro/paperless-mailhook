module github.com/Syfaro/paperless-mailhook

go 1.16

replace github.com/jordan-wright/email => github.com/erply/email v4.0.4-0.20210316103706-deb43c137656+incompatible

require (
	github.com/joho/godotenv v1.3.0
	github.com/jordan-wright/email v4.0.1-0.20210109023952-943e75fe5223+incompatible
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/sirupsen/logrus v1.8.1
	github.com/thecodingmachine/gotenberg-go-client/v7 v7.1.0
)
