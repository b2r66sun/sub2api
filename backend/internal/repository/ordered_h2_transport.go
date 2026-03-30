package repository

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	fhttp2 "github.com/bogdanfinn/fhttp/http2"
	utls "github.com/bogdanfinn/utls"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

// orderedH2Transport wraps bogdanfinn/fhttp's HTTP/2 transport to provide
// header ordering control while implementing the standard net/http.RoundTripper
// interface. This ensures HTTP/2 HPACK-encoded headers are sent in the same
// order as a real Claude Code (Node.js) client.
type orderedH2Transport struct {
	transport *fhttp2.Transport
}

// newOrderedH2Transport creates an HTTP/2 transport that sends headers in
// Claude Code wire order and pseudo-headers in Node.js order.
// HTTP/2 SETTINGS are configured to match Node.js/nghttp2 defaults.
func newOrderedH2Transport(
	dialTLS func(ctx context.Context, network, addr string) (net.Conn, error),
) *orderedH2Transport {
	t := &fhttp2.Transport{
		DialTLS: func(network, addr string, _ *utls.Config) (net.Conn, error) {
			return dialTLS(context.Background(), network, addr)
		},
		PseudoHeaderOrder: claude.PseudoHeaderOrder,

		// HTTP/2 SETTINGS — match Node.js (nghttp2) client defaults
		HeaderTableSize:   4096,  // SETTINGS_HEADER_TABLE_SIZE: spec default
		InitialWindowSize: 65535, // SETTINGS_INITIAL_WINDOW_SIZE: spec default
		// ConnectionFlow: 0 → uses fhttp default 15663105 (~15MB)
		Settings: map[fhttp2.SettingID]uint32{
			fhttp2.SettingEnablePush:           0,     // client disables server push
			fhttp2.SettingMaxConcurrentStreams:  100,   // nghttp2 client default
			fhttp2.SettingMaxFrameSize:          16384, // spec default
		},
		// SETTINGS frame field order — numerical, matching nghttp2
		SettingsOrder: []fhttp2.SettingID{
			fhttp2.SettingHeaderTableSize,
			fhttp2.SettingEnablePush,
			fhttp2.SettingMaxConcurrentStreams,
			fhttp2.SettingInitialWindowSize,
			fhttp2.SettingMaxFrameSize,
		},

		ReadIdleTimeout: 30 * time.Second,
		PingTimeout:     15 * time.Second,
	}
	return &orderedH2Transport{transport: t}
}

// RoundTrip converts the standard net/http.Request to an fhttp.Request with
// header ordering, delegates to the fhttp HTTP/2 transport, and converts the
// response back.
func (t *orderedH2Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	fReq := toFHTTPRequest(req)
	fResp, err := t.transport.RoundTrip(fReq)
	if err != nil {
		return nil, err
	}
	return fromFHTTPResponse(fResp, req), nil
}

// toFHTTPRequest converts a net/http.Request to an fhttp.Request with header ordering.
func toFHTTPRequest(req *http.Request) *fhttp.Request {
	fh := make(fhttp.Header, len(req.Header)+1)
	for k, v := range req.Header {
		fh[k] = v
	}

	// Build header order: known wire-order keys first, then any remaining keys
	var order []string
	present := make(map[string]bool, len(req.Header))
	for k := range req.Header {
		present[strings.ToLower(k)] = true
	}
	for _, k := range claude.HeaderWireOrder {
		lk := strings.ToLower(k)
		if present[lk] {
			order = append(order, lk)
		}
	}
	for k := range req.Header {
		lk := strings.ToLower(k)
		found := false
		for _, wk := range claude.HeaderWireOrder {
			if strings.ToLower(wk) == lk {
				found = true
				break
			}
		}
		if !found {
			order = append(order, lk)
		}
	}
	fh[fhttp.HeaderOrderKey] = order

	fReq := &fhttp.Request{
		Method:           req.Method,
		URL:              req.URL,
		Proto:            req.Proto,
		ProtoMajor:       req.ProtoMajor,
		ProtoMinor:       req.ProtoMinor,
		Header:           fh,
		Body:             req.Body,
		GetBody:          req.GetBody,
		ContentLength:    req.ContentLength,
		TransferEncoding: req.TransferEncoding,
		Close:            req.Close,
		Host:             req.Host,
		Trailer:          fhttp.Header(req.Trailer),
	}
	return fReq.WithContext(req.Context())
}

// fromFHTTPResponse converts an fhttp.Response back to a net/http.Response.
func fromFHTTPResponse(fResp *fhttp.Response, origReq *http.Request) *http.Response {
	return &http.Response{
		Status:           fResp.Status,
		StatusCode:       fResp.StatusCode,
		Proto:            fResp.Proto,
		ProtoMajor:       fResp.ProtoMajor,
		ProtoMinor:       fResp.ProtoMinor,
		Header:           http.Header(fResp.Header),
		Body:             fResp.Body,
		ContentLength:    fResp.ContentLength,
		TransferEncoding: fResp.TransferEncoding,
		Close:            fResp.Close,
		Uncompressed:     fResp.Uncompressed,
		Trailer:          http.Header(fResp.Trailer),
		Request:          origReq,
	}
}
