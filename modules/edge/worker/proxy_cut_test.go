package worker

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
)

// The key proxy and a provider that fails a job. 2026-11-11: one reset of a
// provider connection ended four jobs of one bench at the same moment, each
// "ZstdDecompressionError" in its harness and "fail: no verdict" on its
// card (three work jobs; the read was a fault with the same words). The
// proxy logged "response body cut short" and told nobody else.

// cutBody_internal is an upstream body: its chunks, one per read, then err
// (nil: the end). onRead runs before each read with the reads before it.
type cutBody_internal struct {
	chunks []string
	err    error
	onRead func(before int)
	reads  int
}

func (b *cutBody_internal) Read(p []byte) (int, error) {
	if b.onRead != nil {
		b.onRead(b.reads)
	}
	b.reads++
	if len(b.chunks) > 0 {
		n := copy(p, b.chunks[0])
		b.chunks = b.chunks[1:]
		return n, nil
	}
	if b.err != nil {
		return 0, b.err
	}
	return 0, io.EOF
}
func (b *cutBody_internal) Close() error { return nil }

var errReset_internal = errors.New("read tcp 10.0.0.2:56931->203.0.113.7:443: read: connection reset by peer")

// stream_internal is an upstream's 200 with chunks, then err (nil: the end).
func stream_internal(err error, chunks ...string) func() *http.Response {
	return func() *http.Response {
		return &http.Response{StatusCode: http.StatusOK, Body: &cutBody_internal{chunks: slices.Clone(chunks), err: err}, Header: http.Header{"Content-Type": {"text/event-stream"}}}
	}
}

// refused_internal is an upstream's answer of code with nothing to stream.
func refused_internal(code int) func() *http.Response {
	return func() *http.Response {
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(`{"error":"overloaded"}`)), Header: http.Header{}}
	}
}

// cutProxy_internal is a proxy with one route whose upstream gives answers
// in turn, the last one again after it; sent, when not nil, takes the
// header the upstream was sent.
func cutProxy_internal(sent *http.Header, answers ...func() *http.Response) *KeyProxy {
	var mu sync.Mutex
	n := 0
	return upstreamProxy_internal(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if sent != nil {
			*sent = r.Header.Clone()
		}
		answer := answers[min(n, len(answers)-1)]
		n++
		return answer(), nil
	})
}

// upstreamProxy_internal is a proxy with one route whose upstream is rt.
func upstreamProxy_internal(rt roundTripFunc) *KeyProxy {
	return &KeyProxy{Transport: rt, Routes: map[string]ProxyRoute{"deepseek": {Name: "deepseek", UpstreamURL: "https://api.deepseek.com", AllowedHosts: []string{"api.deepseek.com"}, Key: func() (string, error) { return "sk-x", nil }}}}
}

// serveCut_internal is one request of job through p, as a harness on Bun
// sends it; aborted is what the handler panicked with (nil: it returned).
func serveCut_internal(ctx context.Context, p *KeyProxy, w http.ResponseWriter, job string) (aborted any) {
	return serveNamed_internal(ctx, p, w, job, "")
}

// serveNamed_internal is serveCut_internal with the request named in its
// query (?r=name), for an upstream that answers each request its own way.
func serveNamed_internal(ctx context.Context, p *KeyProxy, w http.ResponseWriter, job, name string) (aborted any) {
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:56931/deepseek/v1/chat/completions?r="+name, strings.NewReader(`{}`)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+ProxyKey(job))
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	defer func() { aborted = recover() }()
	p.ServeHTTP(w, req)
	return nil
}

// The night's run through the runner: the provider fails the harness's
// request, the harness dies of it with no result.json, long after the launch
// grace. That is the provider's fault, dealt again with no attempt spent,
// for a work job as for a read: never "fail: no verdict" on the card. A
// provider failure the harness got past (its next request was answered) is
// not what ended the run: that run's missing verdict is its own.
func TestKeyProxyAProviderFailureThatEndsARunIsAFaultNeverTheCardsFail(t *testing.T) {
	t.Parallel()
	cut := stream_internal(errReset_internal, "data: one\n\n")
	for _, c := range []struct {
		name     string
		requests int
		answers  []func() *http.Response
		want     map[string]string // by kind: the result, a space, words of the reason
	}{
		{"a response cut mid-body", 1, []func() *http.Response{cut}, map[string]string{"read": "fault provider HTTP 502", "work": "fault provider HTTP 502"}},
		{"a 503", 1, []func() *http.Response{refused_internal(503)}, map[string]string{"read": "fault provider HTTP 503", "work": "fault provider HTTP 503"}},
		{"a cut the harness got past", 2, []func() *http.Response{cut, stream_internal(nil, "data: [DONE]\n\n")}, map[string]string{"read": "fault no verdict", "work": "fail no verdict"}},
	} {
		for _, kind := range []string{"read", "work"} {
			p := cutProxy_internal(nil, c.answers...)
			dies := &liveWall{fn: func(ctx context.Context, j Job) (WallRunResult, error) {
				for range c.requests {
					serveCut_internal(ctx, p, httptest.NewRecorder(), j.Consumer)
				}
				return WallRunResult{ExitCode: 1, Elapsed: 20 * time.Minute, Output: []byte("ZstdDecompressionError fetching http://127.0.0.1:56931/deepseek/v1/chat/completions")}, nil
			}}
			sp := &cr2Sprint{kind: kind, next: NextResponse{Row: "vision.ds", State: card.Up, Width: 1, Ready: []ConsumerSummary{{ID: "p1.c1", Kind: kind, Gen: 2}}}}
			r, _ := liveWorkRunner_internal(t, sp, dies)
			r.Proxy = p
			if err := r.RunConsumer(context.Background(), sp.next, sp.next.Ready[0]); err != nil {
				t.Logf("%s, %s: RunConsumer: %v", c.name, kind, err)
			}
			result, words, _ := strings.Cut(c.want[kind], " ")
			reps := sp.reported()
			if len(reps) != 1 || reps[0].Result != result || !strings.Contains(reps[0].Reason, words) {
				t.Errorf("%s job ended by %s: want one %s naming %q, got %+v", kind, c.name, result, words, reps)
			}
		}
	}
}

// The cut is the job's whose request it was, and the harness is not handed
// a short body that ends cleanly: the handler aborts the connection. A
// harness that hung up itself is no provider failure, a whole body records
// none, and an answered request ends the failure before it.
func TestKeyProxyCutBodyIsTheJobsOwnAndAbortsTheResponse(t *testing.T) {
	t.Parallel()
	p := cutProxy_internal(nil, stream_internal(errReset_internal, "data: one\n\n"), stream_internal(nil, "data: one\n\n", "data: [DONE]\n\n"))
	rec := httptest.NewRecorder()
	if aborted := serveCut_internal(context.Background(), p, rec, "p1.a1"); aborted != http.ErrAbortHandler {
		t.Errorf("a cut body ended the handler with %v: want http.ErrAbortHandler, so the harness sees a broken connection and not a short body that ends cleanly", aborted)
	}
	if got := rec.Body.String(); got != "data: one\n\n" {
		t.Errorf("the harness was sent %q before the cut, want what the upstream sent", got)
	}
	if got := p.ConsumeStatus("p2.b1"); got != 0 {
		t.Errorf("job p2.b1 consumed status %d from job p1.a1's cut", got)
	}
	if got := p.ConsumeStatus("p1.a1"); got != http.StatusBadGateway {
		t.Errorf("job p1.a1 consumed %d after its response was cut, want 502", got)
	}

	p = cutProxy_internal(nil, stream_internal(errReset_internal, "data: one\n\n"), stream_internal(nil, "data: one\n\n", "data: [DONE]\n\n"))
	serveCut_internal(context.Background(), p, httptest.NewRecorder(), "p1.a1")
	rec = httptest.NewRecorder()
	if aborted := serveCut_internal(context.Background(), p, rec, "p1.a1"); aborted != nil || rec.Body.String() != "data: one\n\ndata: [DONE]\n\n" {
		t.Errorf("a whole body: aborted %v, body %q: want it sent whole", aborted, rec.Body.String())
	}
	if got := p.ConsumeStatus("p1.a1"); got != 0 {
		t.Errorf("job p1.a1 consumed %d after a cut and then an answered request: the harness got past the cut", got)
	}

	gone, hangUp := context.WithCancel(context.Background())
	hangUp()
	q := cutProxy_internal(nil, stream_internal(context.Canceled, "data: one\n\n"))
	serveCut_internal(gone, q, httptest.NewRecorder(), "p1.a1")
	if got := q.ConsumeStatus("p1.a1"); got != 0 {
		t.Errorf("a harness that hung up was recorded as provider status %d: that is no provider failure", got)
	}
}

// The harness's Accept-Encoding is not the proxy's to forward: forwarded,
// net/http hands the upstream's zstd or br through undecoded, and a cut is a
// decompression error inside the harness. Not forwarded, the transport asks
// for what it decodes itself and the harness is sent plain bytes.
func TestKeyProxyDoesNotForwardTheHarnessAcceptEncoding(t *testing.T) {
	t.Parallel()
	var sent http.Header
	p := cutProxy_internal(&sent, stream_internal(nil, "{}"))
	serveCut_internal(context.Background(), p, httptest.NewRecorder(), "p1.a1")
	if got := sent.Get("Accept-Encoding"); got != "" {
		t.Errorf("the upstream was sent Accept-Encoding %q from the harness: the transport then decodes nothing and the harness gets the compressed body", got)
	}
	if got := sent.Get("Authorization"); got != "Bearer sk-x" {
		t.Errorf("the upstream was sent Authorization %q: want the route's key", got)
	}
}

// A stream's event reaches the harness when the upstream sends it: by the
// time the proxy asks the upstream for more, what it has is flushed.
// Unflushed, an event waits in the server's buffer for 4 KiB or the end.
func TestKeyProxyFlushesAnEventBeforeWaitingForTheNext(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	flushedBeforeSecondRead := false
	p := cutProxy_internal(nil, func() *http.Response {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: &cutBody_internal{chunks: []string{"data: one\n\n", "data: two\n\n"}, onRead: func(before int) {
			if before == 1 {
				flushedBeforeSecondRead = rec.Flushed && rec.Body.String() == "data: one\n\n"
			}
		}}}
	})
	serveCut_internal(context.Background(), p, rec, "p1.a1")
	if !flushedBeforeSecondRead {
		t.Errorf("the proxy waited on the upstream for a second event with the first not flushed to the harness")
	}
}

// A harness that hangs up before the provider answers is no provider
// failure: one that ends its run with a good head, cancelling a request on
// its way out, is not dealt again as "provider HTTP 502". An upstream that
// cannot be reached, with the harness still waiting, is one.
func TestKeyProxyAHarnessHangUpBeforeTheAnswerIsNoProviderFailure(t *testing.T) {
	t.Parallel()
	unanswered := func(err error) *KeyProxy {
		return upstreamProxy_internal(func(*http.Request) (*http.Response, error) { return nil, err })
	}
	gone, hangUp := context.WithCancel(context.Background())
	hangUp()
	p := unanswered(context.Canceled)
	serveCut_internal(gone, p, httptest.NewRecorder(), "p1.a1")
	if got := p.ConsumeStatus("p1.a1"); got != 0 {
		t.Errorf("a harness that hung up before the answer was recorded as provider status %d: that is no provider failure", got)
	}
	q, rec := unanswered(errors.New("dial tcp 203.0.113.7:443: connect: connection refused")), httptest.NewRecorder()
	serveCut_internal(context.Background(), q, rec, "p1.a1")
	if got := q.ConsumeStatus("p1.a1"); got != http.StatusBadGateway || rec.Code != http.StatusBadGateway {
		t.Errorf("an upstream that could not be reached: status %d, the harness was sent %d: want 502 for both", got, rec.Code)
	}
}

// Two requests of one job in flight at once. A request sent before another's
// failure and answered after it is no sign the harness got past the failure:
// only an answer to a request sent after the failure is. And a cut that
// follows an answer to a later request is still the job's failure: the long
// stream that is cut may be the one the harness dies of.
func TestKeyProxyOnlyARequestSentAfterAFailureEndsIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	whole := stream_internal(nil, "data: [DONE]\n\n")

	// b is sent, a is cut, b is answered
	bSent, aCut, bDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	p := upstreamProxy_internal(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("r") == "b" {
			close(bSent)
			<-aCut
			return whole(), nil
		}
		return stream_internal(errReset_internal, "data: one\n\n")(), nil
	})
	go func() {
		defer close(bDone)
		serveNamed_internal(ctx, p, httptest.NewRecorder(), "p1.a1", "b")
	}()
	<-bSent
	serveNamed_internal(ctx, p, httptest.NewRecorder(), "p1.a1", "a")
	close(aCut)
	<-bDone
	if got := p.ConsumeStatus("p1.a1"); got != http.StatusBadGateway {
		t.Errorf("a cut, then an answer to a request sent before the cut: status %d, want the cut's 502: that answer hides the failure the harness died of", got)
	}

	// a is sent and streams, b is sent and answered whole, a is cut
	aSent, bWhole, aDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	p = upstreamProxy_internal(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("r") == "b" {
			return whole(), nil
		}
		close(aSent)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: &cutBody_internal{chunks: []string{"data: one\n\n"}, err: errReset_internal, onRead: func(before int) {
			if before == 1 {
				<-bWhole
			}
		}}}, nil
	})
	go func() {
		defer close(aDone)
		serveNamed_internal(ctx, p, httptest.NewRecorder(), "p1.a1", "a")
	}()
	<-aSent
	serveNamed_internal(ctx, p, httptest.NewRecorder(), "p1.a1", "b")
	close(bWhole)
	<-aDone
	if got := p.ConsumeStatus("p1.a1"); got != http.StatusBadGateway {
		t.Errorf("an answer to a later request, then the first one's cut: status %d, want the cut's 502", got)
	}

	// a is cut, then b is sent and answered: the harness got past it
	p = cutProxy_internal(nil, stream_internal(errReset_internal, "data: one\n\n"), whole)
	serveNamed_internal(ctx, p, httptest.NewRecorder(), "p1.a1", "a")
	serveNamed_internal(ctx, p, httptest.NewRecorder(), "p1.a1", "b")
	if got := p.ConsumeStatus("p1.a1"); got != 0 {
		t.Errorf("a cut, then a request sent after it and answered: status %d, want none", got)
	}
}

// A body past the proxy's cap is cut and the response aborted: the harness
// is not handed 16 MiB that end cleanly as if they were the whole of it. A
// body of exactly the cap is whole.
func TestKeyProxyABodyOverTheCapIsAbortedNeverEndedCleanly(t *testing.T) {
	t.Parallel()
	const limit = 16 << 20
	body := func(n int) func() *http.Response {
		return func() *http.Response {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", n)))}
		}
	}
	rec := httptest.NewRecorder()
	if aborted := serveCut_internal(context.Background(), cutProxy_internal(nil, body(limit+1)), rec, "p1.a1"); aborted != http.ErrAbortHandler || rec.Body.Len() != limit {
		t.Errorf("a body one byte over the cap: the handler ended with %v after %d bytes: want http.ErrAbortHandler after %d", aborted, rec.Body.Len(), limit)
	}
	rec = httptest.NewRecorder()
	if aborted := serveCut_internal(context.Background(), cutProxy_internal(nil, body(limit)), rec, "p1.a1"); aborted != nil || rec.Body.Len() != limit {
		t.Errorf("a body of exactly the cap: the handler ended with %v after %d bytes: want it whole", aborted, rec.Body.Len())
	}
}

// The proxy's own transport, as built: HTTP/1.1 only, and an idle
// connection kept for each job of a full row and its side requests, so a
// request does not pay a TLS handshake.
func TestKeyProxyUpstreamTransportIsHTTP1AndKeepsARowsConnections(t *testing.T) {
	t.Parallel()
	tr := newUpstream_internal()
	if tr.Protocols == nil || !tr.Protocols.HTTP1() || tr.Protocols.HTTP2() || tr.Protocols.UnencryptedHTTP2() {
		t.Errorf("the upstream transport's protocols are %v: want HTTP/1.1 only", tr.Protocols)
	}
	if tr.MaxIdleConnsPerHost != 32 {
		t.Errorf("the upstream transport keeps %d idle connections per provider (0: net/http's 2): want 32", tr.MaxIdleConnsPerHost)
	}
}

type connKey_internal struct{}

// Two jobs stream through one proxy from one provider over real
// connections (the upstream is TLS and offers HTTP/2, as a provider does);
// the provider resets the connection under one job's response. That job's
// harness sees a broken response and the job a provider 502; the other
// job's stream goes on to its end. Each first event is in the harness's
// hands while its stream is still open.
func TestFunctionalKeyProxyUpstreamResetCutsOneJobOnly(t *testing.T) {
	t.Parallel()
	release, cutSeen := make(chan struct{}), make(chan struct{})
	var releaseOnce, cutSeenOnce sync.Once
	doRelease := func() { releaseOnce.Do(func() { close(release) }) }
	doCutSeen := func() { cutSeenOnce.Do(func() { close(cutSeen) }) }
	var mu sync.Mutex
	asked := map[string]string{}
	up := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked[r.URL.Path] = r.Proto + " accept-encoding=" + r.Header.Get("Accept-Encoding")
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: one\n\n")
		w.(http.Flusher).Flush()
		<-release
		if strings.HasSuffix(r.URL.Path, "/cut") {
			// a reset, as the provider's: no close notify, no last chunk
			tcp := r.Context().Value(connKey_internal{}).(*tls.Conn).NetConn().(*net.TCPConn)
			if err := tcp.SetLinger(0); err != nil {
				t.Errorf("SetLinger: %v", err)
			}
			if err := tcp.Close(); err != nil {
				t.Errorf("reset: %v", err)
			}
			return
		}
		<-cutSeen
		fmt.Fprint(w, "data: two\n\n")
	}))
	up.EnableHTTP2 = true
	up.Config.ConnContext = func(ctx context.Context, c net.Conn) context.Context {
		return context.WithValue(ctx, connKey_internal{}, c)
	}
	up.StartTLS()
	defer up.Close()

	// the transport the proxy uses when given none, trusting the test's upstream
	tr := newUpstream_internal()
	tr.TLSClientConfig.RootCAs = up.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	defer tr.CloseIdleConnections()
	p := &KeyProxy{Transport: tr, Routes: map[string]ProxyRoute{"up": {Name: "up", UpstreamURL: up.URL, AllowedHosts: []string{"127.0.0.1"}, Key: func() (string, error) { return "sk-x", nil }}}}
	front := httptest.NewServer(p)
	defer front.Close()
	defer doCutSeen() // a failed test leaves no handler waiting
	defer doRelease()

	type end struct {
		rest string
		err  error
	}
	first, ends := map[string]chan error{}, map[string]chan end{}
	for _, job := range []string{"keep", "cut"} {
		first[job], ends[job] = make(chan error, 1), make(chan end, 1)
		go func() {
			req, err := http.NewRequest(http.MethodPost, front.URL+"/up/v1/"+job, strings.NewReader(`{}`))
			if err != nil {
				first[job] <- err
				return
			}
			req.Header.Set("Authorization", "Bearer "+ProxyKey("p1."+job))
			req.Header.Set("Accept-Encoding", "zstd")
			resp, err := front.Client().Do(req)
			if err != nil {
				first[job] <- err
				return
			}
			defer resp.Body.Close()
			br := bufio.NewReader(resp.Body)
			line, err := br.ReadString('\n')
			if err == nil && line != "data: one\n" {
				err = fmt.Errorf("first line %q", line)
			}
			first[job] <- err
			rest, err := io.ReadAll(br)
			ends[job] <- end{string(rest), err}
		}()
	}
	for _, job := range []string{"keep", "cut"} {
		select {
		case err := <-first[job]:
			if err != nil {
				t.Fatalf("job %s: its first event: %v", job, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("job %s: its first event did not reach the harness in 10 s with the stream open: the proxy holds it unflushed", job)
		}
	}
	doRelease()
	select {
	case e := <-ends["cut"]:
		if e.err == nil {
			t.Errorf("the cut job's harness read a clean end after %q: want a broken response it can tell from a finished one", e.rest)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("the cut job's response did not end in 10 s")
	}
	doCutSeen()
	select {
	case e := <-ends["keep"]:
		if e.err != nil || !strings.Contains(e.rest, "data: two") {
			t.Errorf("the other job's stream after the reset: %q, %v: want it to go on to its end (one connection per request, never the bench's one)", e.rest, e.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("the other job's response did not end in 10 s")
	}
	if got := p.ConsumeStatus("p1.cut"); got != http.StatusBadGateway {
		t.Errorf("the cut job consumed status %d, want 502", got)
	}
	if got := p.ConsumeStatus("p1.keep"); got != 0 {
		t.Errorf("the other job consumed status %d from its neighbour's reset", got)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, path := range []string{"/v1/keep", "/v1/cut"} {
		if asked[path] != "HTTP/1.1 accept-encoding=gzip" {
			t.Errorf("the upstream was asked %s as %q: want HTTP/1.1 (a connection of its own) and the transport's own gzip", path, asked[path])
		}
	}
}
