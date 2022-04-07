// Package apig provides an adapter enabling use of http.Handler inside AWS
// Lambda running as AWS API Gateway HTTP API target. It also supports Lambda
// Function URLs.
//
// For more context see
// https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api.html
// and https://docs.aws.amazon.com/lambda/latest/dg/lambda-urls.html
//
// Usage example:
//
//  package main
//
//  import (
//      "net/http"
//
//      "github.com/artyom/apig"
//      "github.com/aws/aws-lambda-go/lambda"
//  )
//
//  func main() {
//      lambda.Start(apig.Handler(http.HandlerFunc(hello)))
//  }
//
//  func hello(w http.ResponseWriter, r *http.Request) {
//      w.Write([]byte("Hello, world!\n"))
//  }
package apig

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/aws/aws-lambda-go/events"
)

// Handler returns function suitable to use as an AWS Lambda handler with
// github.com/aws/aws-lambda-go/lambda package.
//
// Note that both request and response are fully cached in memory.
func Handler(h http.Handler) func(context.Context, *events.APIGatewayV2HTTPRequest) (*events.APIGatewayV2HTTPResponse, error) {
	if h == nil {
		panic("Handler called with nil argument")
	}
	hh := &lambdaHandler{handler: h}
	return hh.Run
}

type lambdaHandler struct {
	handler http.Handler
}

func (h *lambdaHandler) Run(ctx context.Context, req *events.APIGatewayV2HTTPRequest) (*events.APIGatewayV2HTTPResponse, error) {
	headers := make(http.Header, len(req.Headers))
	for k, v := range req.Headers {
		headers.Set(k, v)
	}
	if len(req.Cookies) != 0 {
		headers[http.CanonicalHeaderKey("Cookie")] = req.Cookies
	}
	r := &http.Request{
		ProtoMajor: 1,
		ProtoMinor: 1,
		Proto:      "HTTP/1.1",
		Method:     req.RequestContext.HTTP.Method,
		URL:        &url.URL{Path: req.RawPath, RawQuery: req.RawQueryString},
		Header:     headers,
		Host:       headers.Get("Host"),
	}
	r = r.WithContext(ctx)
	switch {
	case req.IsBase64Encoded:
		b, err := base64.StdEncoding.DecodeString(req.Body)
		if err != nil {
			return nil, err
		}
		r.Body = io.NopCloser(bytes.NewReader(b))
		r.ContentLength = int64(len(b))
	default:
		r.Body = io.NopCloser(strings.NewReader(req.Body))
		r.ContentLength = int64(len(req.Body))
	}
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, r)
	res := recorder.Result()
	out := &events.APIGatewayV2HTTPResponse{
		StatusCode: res.StatusCode,
		Headers:    make(map[string]string),
	}
	for k, vv := range res.Header {
		if strings.EqualFold(k, "Set-Cookie") {
			out.Cookies = append(out.Cookies, vv...)
			continue
		}
		if len(vv) == 1 {
			out.Headers[k] = vv[0]
			continue
		}
		if out.MultiValueHeaders == nil {
			out.MultiValueHeaders = make(map[string][]string)
		}
		out.MultiValueHeaders[k] = append(out.MultiValueHeaders[k], vv...)
	}
	if b := recorder.Body.Bytes(); utf8.Valid(b) {
		out.Body = string(b)
	} else {
		out.Body = base64.StdEncoding.EncodeToString(b)
		out.IsBase64Encoded = true
	}
	return out, nil
}
