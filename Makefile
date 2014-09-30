all: cloudproxy

ca:
	@mkdir -p pki/CA/
	@mkdir -p pki/CA/certs
	@mkdir -p pki/CA/crl
	@mkdir -p pki/CA/newcerts
	@mkdir -p pki/CA/private
	@touch pki/CA/index.txt
	@echo 1000 > pki/CA/serial
	@openssl genrsa -aes256 -out pki/CA/private/ca.key.pem 4096
	@openssl req -new -x509 -days 3650 -key pki/CA/private/ca.key.pem -extensions v3_ca -out pki/CA/certs/ca.cert.pem
	@openssl rsa -in pki/CA/private/ca.key.pem -out pki/CA/private/ca.key.pem.clear

cloudproxy:
	@mkdir -p bin/
	@go get cloudproxy
	@cp pki/CA/certs/ca.cert.pem src/github.com/elazarl/goproxy/ca.pem
	@cp pki/CA/private/ca.key.pem.clear src/github.com/elazarl/goproxy/key.pem
	@go install cloudproxy

clean:
	@rm -rf bin/ pkg/

.PHONY: all clean cloudproxy ca
