module github.com/Syfaro/paperless-mailhook

go 1.16

replace github.com/jordan-wright/email => github.com/erply/email v4.0.4-0.20210316103706-deb43c137656+incompatible

require (
	github.com/VictoriaMetrics/metrics v1.17.3
	github.com/joho/godotenv v1.3.0
	github.com/jordan-wright/email v4.0.1-0.20210109023952-943e75fe5223+incompatible
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/kr/pretty v0.1.0 // indirect
	github.com/sirupsen/logrus v1.8.1
	github.com/stretchr/testify v1.7.0
	github.com/thecodingmachine/gotenberg-go-client/v7 v7.2.0
	golang.org/x/sys v0.0.0-20210903071746-97244b99971b // indirect
	gopkg.in/check.v1 v1.0.0-20190902080502-41f04d3bba15 // indirect
)
