package pipeline

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

type Streamer struct {
	endpoint   string
	authToken  string
	ringBuf    *RingBuffer
	client     *http.Client
	logf       func(string, ...any)
	wg         sync.WaitGroup
	batchSize  int
	stopped    chan struct{}
}

func NewStreamer(endpoint, authToken string, ringBuf *RingBuffer,
	pollPeriod time.Duration, logf func(string, ...any),
) *Streamer {

	tr := &http.Transport{
		ForceAttemptHTTP2: true,
		MaxIdleConns:      10,
		IdleConnTimeout:   90 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	return &Streamer{
		endpoint:  endpoint,
		authToken: authToken,
		ringBuf:   ringBuf,
		client: &http.Client{
			Transport: tr,
			Timeout:   30 * time.Second,
		},
		logf:      logf,
		batchSize: 100,
		stopped:   make(chan struct{}),
	}
}

func (s *Streamer) Start(ctx context.Context) {
	s.wg.Add(1)
	go s.run(ctx)
}

func (s *Streamer) Wait() {
	s.wg.Wait()
}

func (s *Streamer) Stopped() <-chan struct{} {
	return s.stopped
}

func (s *Streamer) run(ctx context.Context) {
	defer s.wg.Done()
	defer close(s.stopped)

	backoff := time.Duration(0)
	maxBackoff := 30 * time.Second

	for {
		if s.ringBuf.Len() == 0 {
			select {
			case <-ctx.Done():
				s.flushRemaining(ctx)
				return
			case <-time.After(500 * time.Millisecond):
				continue
			}
		}

		sent, retryable := s.sendBatch(ctx)

		if retryable && sent > 0 {
			backoff = nextBackoff(backoff, maxBackoff)
			s.logf("streamer: retryable error, backing off %v", backoff)
			select {
			case <-ctx.Done():
				s.flushRemaining(ctx)
				return
			case <-time.After(backoff):
			}
		} else {
			backoff = 0
		}

		if sent == 0 {
			select {
			case <-ctx.Done():
				s.flushRemaining(ctx)
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
}

func (s *Streamer) sendBatch(ctx context.Context) (sent int, retryable bool) {
	payloads := s.ringBuf.Drain(s.batchSize)
	if len(payloads) == 0 {
		return 0, false
	}

	errored := false
	for i, data := range payloads {
		if err := s.sendOne(ctx, data); err != nil {
			s.logf("streamer: send failed (%d/%d): %v", i+1, len(payloads), err)
			// Re-push remaining payloads back into ring buffer
			for j := i; j < len(payloads); j++ {
				s.ringBuf.Push(payloads[j])
			}
			errored = true
			break
		}
		sent++
	}

	return sent, errored
}

func (s *Streamer) sendOne(ctx context.Context, data []byte) error {
	if s.endpoint == "" {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.authToken)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	if resp.StatusCode >= 500 {
		return &retryableError{msg: resp.Status}
	}

	return nil
}

func (s *Streamer) flushRemaining(ctx context.Context) {
	for {
		payloads := s.ringBuf.Drain(s.batchSize)
		if len(payloads) == 0 {
			return
		}

		for _, data := range payloads {
			if err := s.sendOne(ctx, data); err != nil {
				s.logf("streamer: flush error: %v", err)
				return
			}
		}

		if s.endpoint == "" {
			return
		}
	}
}

type retryableError struct {
	msg string
}

func (e *retryableError) Error() string {
	return e.msg
}

func nextBackoff(current, max time.Duration) time.Duration {
	if current == 0 {
		return time.Second
	}
	next := current * 2
	if next > max {
		next = max
	}
	return next
}

