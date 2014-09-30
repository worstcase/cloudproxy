package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/elazarl/goproxy"
	"github.com/rcrowley/go-metrics"
)

type ProxyUserData struct {
	RequestID     string
	ContentLength int64
	SourceIP      string
}

type MetricConfig struct {
	Server    string
	Prefix    string
	BatchSize int
	Registry  *metrics.Registry
	Channel   chan GraphiteMetric
}

type GraphiteMetric struct {
	Name      string
	Value     int64
	Timestamp int64
}

type CountReadCloser struct {
	R            io.ReadCloser
	nr           int64
	metricConfig MetricConfig
	userData     ProxyUserData
}

func (g *GraphiteMetric) toString() (txt string) {
	txt = fmt.Sprintf("%s %d %d", g.Name, g.Value, g.Timestamp)
	return txt
}

func sendToGraphite(graphiteChan chan GraphiteMetric, graphiteServer *string) {
	log.Printf("Starting metric sender channel")
	if *graphiteServer != "" {
		log.Printf("Graphite server defined: %s. Will send to graphite.", *graphiteServer)
		addr, _ := net.ResolveTCPAddr("tcp", *graphiteServer)
		conn, err := net.DialTCP("tcp", nil, addr)
		if err != nil {
			log.Printf("Error making initial connection to graphite: %s", err)
		}
		defer conn.Close()
		w := bufio.NewWriter(conn)
		for m := range graphiteChan {
			string_metric := m.toString()
			fmt.Fprintf(w, "%s\n", string_metric)
			w.Flush()
		}
	} else {
		log.Printf("No graphite server defined. Sending to log")
		for m := range graphiteChan {
			string_metric := m.toString()
			log.Printf("%s\n", string_metric)
		}
	}
}

func (c *CountReadCloser) Read(b []byte) (n int, err error) {
	// This needs a serious refactor
	// The response is delayed based on metrics being sent over.
	// Too much work being done here
	n, err = c.R.Read(b)
	c.nr += int64(n)
	// we should implement the following "soon"
	//time_metric_name := c.metricConfig.Prefix + ".proxy_overhead"
	count_metric_name := c.metricConfig.Prefix + ".hits"
	resp_byte_metric_name := c.metricConfig.Prefix + ".response_bytes"
	req_byte_metric_name := c.metricConfig.Prefix + ".request_bytes"
	counter := metrics.GetOrRegisterCounter(count_metric_name, *c.metricConfig.Registry)
	counter.Inc(1)
	var gm GraphiteMetric
	var cm GraphiteMetric
	var clm GraphiteMetric
	timestamp := time.Now().Unix()
	gm.Name = resp_byte_metric_name
	cm.Name = count_metric_name
	clm.Name = req_byte_metric_name
	cm.Value = counter.Snapshot().Count()
	gm.Value = c.nr
	clm.Value = c.userData.ContentLength
	gm.Timestamp = timestamp
	cm.Timestamp = timestamp
	clm.Timestamp = timestamp

	c.metricConfig.Channel <- cm
	c.metricConfig.Channel <- gm
	c.metricConfig.Channel <- clm
	return
}

func (c *CountReadCloser) Close() error {
	return c.R.Close()
}

func orPanic(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	cert_pem_file := flag.String("pemfile", "pki/CA/certs/ca.cert.pem", "Your MITM CA pem")
	cert_key_file := flag.String("keyfile", "pki/CA/private/ca.key.pem.clear", "Your MITM CA pem")
	listen_address := flag.String("address", "127.0.0.1", "IP to listen on")
	proxy_port := flag.Int("port", 3128, "port to listen on")
	tracking_id_header := flag.String("tracking_header", "x-dasein-id", "The header to use for correlating requests")
	batch_size := flag.Int("batch_size", 1000, "The size of the buffer for sending to graphite. Metrics beyond this will block the proxy!")
	graphite_server := flag.String("graphite_server", "", "ip:port of the graphite server to use")
	metric_prefix := flag.String("metric_prefix", "cloudproxy", "The prefix for all metrics")
	debug := flag.Bool("debug", false, "Enable debug logging (warning really noisy!)")
	flag.Parse()
	r := metrics.NewRegistry()
	graphiteChan := make(chan GraphiteMetric, *batch_size)
	go sendToGraphite(graphiteChan, graphite_server)
	// Configure to use our certs instead of the built-in certs
	ca_cert, err := ioutil.ReadFile(*cert_pem_file)
	orPanic(err)
	ca_key, err := ioutil.ReadFile(*cert_key_file)
	orPanic(err)
	goproxy.GoproxyCa, err = tls.X509KeyPair(ca_cert, ca_key)
	orPanic(err)

	// Somebody set us up the proxy
	proxy := goproxy.NewProxyHttpServer()

	// Always MITM SSL
	proxy.OnRequest(goproxy.ReqHostMatches(regexp.MustCompile("^.*$"))).
		HandleConnect(goproxy.AlwaysMitm)

	proxy.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		ctx.Logf("Proxying URL: %s", req.URL.String())
		/* The `ctx.UserData` allows you to track state for a single request
		/ all the way through the pipeline.
		/ Here we add some metrics we want to emit at the end to that "bucket"
		*/
		user_data := ProxyUserData{}
		user_data.RequestID = req.Header.Get(*tracking_id_header)
		user_data.ContentLength = req.ContentLength
		user_data.SourceIP = req.RemoteAddr
		ctx.UserData = user_data
		return req, nil
	})

	// When we get a response, gather our metrics and then send response to client
	proxy.OnResponse().DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		metricConfig := MetricConfig{}
		metricConfig.Channel = graphiteChan
		metricConfig.BatchSize = *batch_size
		metricConfig.Registry = &r
		ctx.Logf("Responding to request")
		hostname := strings.Replace(ctx.Req.Host, ".", "_", -1)
		hostname = strings.Replace(hostname, ":", "_", -1)
		user_data := ctx.UserData.(ProxyUserData)
		request_tracking_id := fmt.Sprint(user_data.RequestID)
		ctx.Logf("Request header is: {\"%s\":\"%s\"}", *tracking_id_header, request_tracking_id)
		var request_id_prefix string
		if request_tracking_id != "" {
			request_id_prefix = *metric_prefix + "." + request_tracking_id + "."
		} else {
			request_id_prefix = *metric_prefix + ".missing_request_id."
		}
		metricConfig.Prefix = request_id_prefix + hostname
		ctx.Logf("Response from URL: %s", ctx.Req.URL.String())

		resp.Body = &CountReadCloser{resp.Body, 0, metricConfig, user_data}
		return resp
	})
	if *debug == true {
		proxy.Verbose = true
	}
	log.Fatal(http.ListenAndServe(*listen_address+":"+strconv.Itoa(*proxy_port), proxy))
}
