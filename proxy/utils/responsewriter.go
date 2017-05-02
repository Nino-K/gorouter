package utils

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
)

type ProxyResponseWriter interface {
	Header() http.Header
	Hijack() (net.Conn, *bufio.ReadWriter, error)
	Write(b []byte) (int, error)
	WriteHeader(s int)
	Done()
	Flush()
	Status() int
	Size() int
	CloseNotify() <-chan bool
}

type proxyResponseWriter struct {
	w      http.ResponseWriter
	status int
	size   int

	flusher http.Flusher
	done    bool
}

func NewProxyResponseWriter(w http.ResponseWriter) *proxyResponseWriter {
	proxyWriter := &proxyResponseWriter{
		w:       w,
		flusher: w.(http.Flusher),
	}

	return proxyWriter
}

func (p *proxyResponseWriter) CloseNotify() <-chan bool {
	if closeNotifier, ok := p.w.(http.CloseNotifier); ok {
		return closeNotifier.CloseNotify()
	}
	return make(chan bool)
}

func (p *proxyResponseWriter) Header() http.Header {
	return p.w.Header()
}

func (p *proxyResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := p.w.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer cannot hijack")
	}
	return hijacker.Hijack()
}

func (p *proxyResponseWriter) Write(b []byte) (int, error) {
	if p.done {
		return 0, nil
	}

	if p.status == 0 {
		fmt.Println("proxy-response-writer-in-WRITE", p.status)
		p.WriteHeader(http.StatusOK)
	}
	size, err := p.w.Write(b)
	p.size += size
	return size, err
}

func (p *proxyResponseWriter) WriteHeader(s int) {
	fmt.Println("proxy-response-writer-writing-status-code", s)
	debug.PrintStack()
	if p.done {
		return
	}

	// if Content-Type not in response, nil out to suppress Go's auto-detect
	if _, ok := p.w.Header()["Content-Type"]; !ok {
		p.w.Header()["Content-Type"] = nil
	}

	p.w.WriteHeader(s)

	if p.status == 0 {
		p.status = s
	}
}

func (p *proxyResponseWriter) Done() {
	p.done = true
}

func (p *proxyResponseWriter) Flush() {
	if p.flusher != nil {
		p.flusher.Flush()
	}
}

func (p *proxyResponseWriter) Status() int {
	return p.status
}

func (p *proxyResponseWriter) Size() int {
	return p.size
}
