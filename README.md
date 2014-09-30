# cloudproxy
A MITM proxy for tracking cloud API calls.

## How it works
`cloudproxy` functions like any other proxy. It doesn't do any caching and logs request data to stdout and optionally can push it to graphite.

It can also "hijack" your SSL connections and peek in to them. It does this like so:

- SSL request comes in. By default proxies will simply act as a dumb conduit as the http client application will use `DIRECT`.
- `cloudproxy` will make the request itself and reencrypt the reponse with its own CA cert.
- It will respond to the requestor with the data reencrypted with its own cert

Normally this would generate an SSL verification warning/error. However if you import the CA certificate `cloudproxy` is using for resigning into your own SSL store as a trusted CA, you'll never know the difference. Meanwhile `cloudproxy` was able to see inside the request and extract information.

## Requirements
- `git`
- `mercurial` _lol googlecode_
- `bzr` _lol launchpad_
- `go`

## Building
Before anything will work, you need a CA certificate generated

- `make ca`

This just wraps the openssl commands. You really should generate a CA certificate per installation. Answer all the questions for your CA's identity.

Now you can build it.

- `make clean all`

- `bin/cloudproxy -h`

```
  -address="127.0.0.1": IP to listen on
  -batch_size=1000: The size of the buffer for sending to graphite. Metrics beyond this will block the proxy!
  -debug=false: Enable debug logging (warning really noisy!)
  -graphite_server="": ip:port of the graphite server to use
  -keyfile="pki/CA/private/ca.key.pem.clear": Your MITM CA pem
  -metric_prefix="cloudproxy": The prefix for all metrics
  -pemfile="pki/CA/certs/ca.cert.pem": Your MITM CA pem
  -port=3128: port to listen on
  -tracking_header="x-dasein-id": The header to use for correlating requests
```

Normally `cloudproxy` will just log data to stdout. If you set `--debug=true`, you'll get even more data. Otherwise you'll just get metrics logged to stdout. 

This is nice and all but the real value comes when logging it to graphite. If you specify `--graphite_server=ip:port` pointing to a graphite (or graphite-compatible) server, it will shove all the collected metrics in there.

Because this was designed to be used as part of performance troubleshooting and metric gathering, cloudproxy can look for any header inside the original request specified with `-tracking_header=XXXXXX`. It will then namespace the metrics it collects based on that header. This allows you to correlate raw request data with some upstream api.

## Verifying
`http_proxy=127.0.0.1:3128 https_proxy=127.0.0.1:3128 curl -Iv https://google.com/`

You should get a failure from curl:

```
* Rebuilt URL to: https://google.com/
* Hostname was NOT found in DNS cache
*   Trying 127.0.0.1...
* Connected to 127.0.0.1 (127.0.0.1) port 8080 (#0)
* Establish HTTP proxy tunnel to google.com:443
> CONNECT google.com:443 HTTP/1.1
> Host: google.com:443
> User-Agent: curl/7.35.0
> Proxy-Connection: Keep-Alive
> 
< HTTP/1.0 200 OK
HTTP/1.0 200 OK
< 

* Proxy replied OK to CONNECT request
* successfully set certificate verify locations:
*   CAfile: none
  CApath: /etc/ssl/certs
* SSLv3, TLS handshake, Client hello (1):
* SSLv3, TLS handshake, Server hello (2):
* SSLv3, TLS handshake, CERT (11):
* SSLv3, TLS alert, Server hello (2):
* SSL certificate problem: self signed certificate in certificate chain
* Closing connection 0
```

Now point curl to your ca cert:
`http_proxy=127.0.0.1:3128 https_proxy=127.0.0.1:8080 curl --cacert pki/CA/certs/ca.cert.pem -Iv https://google.com`

Oh look, it validates!:

```
* Rebuilt URL to: https://google.com/
* Hostname was NOT found in DNS cache
*   Trying 127.0.0.1...
* Connected to 127.0.0.1 (127.0.0.1) port 8080 (#0)
* Establish HTTP proxy tunnel to google.com:443
> CONNECT google.com:443 HTTP/1.1
> Host: google.com:443
> User-Agent: curl/7.35.0
> Proxy-Connection: Keep-Alive
> 
< HTTP/1.0 200 OK
HTTP/1.0 200 OK
< 
```

## Verifying with Java
To verify/use this with java, you'll need to import the CA pem as a trusted root into the keystore used by your jvm. On ubuntu, this defaults to `/etc/ssl/certs/java/cacerts`. The default keystore password is `changeit`.

You can import the CA pem with the following invocation:

`keytool -import -trustcacerts -alias CloudProxyExternalCARoot -file pki/CA/certs/ca.cert.pem -keystore /etc/ssl/certs/java/cacerts`

Now when you start your JVM and use the proxy (the method for specifying this is different per java application), you'll be going through the proxy.
