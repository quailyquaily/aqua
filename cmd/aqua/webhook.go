package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/aqua/aqua"
)

const (
	webhookDispatchTimeout      = 10 * time.Second
	webhookRetryInitialBackoff  = time.Second
	webhookRetryMaximumBackoff  = 2 * time.Minute
	webhookResponseBodyMaxBytes = 512
)

type webhookQueueItem struct {
	body           []byte
	fromPeerID     string
	topic          string
	idempotencyKey string
	receivedAt     time.Time
}

type dataPushWebhookDispatcher struct {
	url                 string
	logger              *slog.Logger
	client              *http.Client
	retryInitialBackoff time.Duration
	retryMaximumBackoff time.Duration

	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	cond   *sync.Cond
	queue  []webhookQueueItem
	closed bool
	wg     sync.WaitGroup
}

func resolveServeWebhookURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("invalid --webhook %q: %w", raw, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("invalid --webhook %q: scheme must be http or https", raw)
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return "", fmt.Errorf("invalid --webhook %q: host is required", raw)
	}
	return parsed.String(), nil
}

func newDataPushWebhookDispatcher(parent context.Context, webhookURL string, logger *slog.Logger) (*dataPushWebhookDispatcher, error) {
	if strings.TrimSpace(webhookURL) == "" {
		return nil, nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(parent)
	dispatcher := &dataPushWebhookDispatcher{
		url:                 webhookURL,
		logger:              logger,
		client:              &http.Client{Timeout: webhookDispatchTimeout},
		retryInitialBackoff: webhookRetryInitialBackoff,
		retryMaximumBackoff: webhookRetryMaximumBackoff,
		ctx:                 ctx,
		cancel:              cancel,
	}
	dispatcher.cond = sync.NewCond(&dispatcher.mu)
	dispatcher.wg.Add(1)
	go func() {
		defer dispatcher.wg.Done()
		dispatcher.run()
	}()
	return dispatcher, nil
}

func (d *dataPushWebhookDispatcher) enqueue(event aqua.DataPushEvent) {
	if d == nil {
		return
	}
	body, err := json.Marshal(dataPushEventView(event))
	if err != nil {
		d.logger.Warn("marshal webhook payload failed", "topic", event.Topic, "from_peer_id", event.FromPeerID, "err", err)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	d.queue = append(d.queue, webhookQueueItem{
		body:           body,
		fromPeerID:     event.FromPeerID,
		topic:          event.Topic,
		idempotencyKey: event.IdempotencyKey,
		receivedAt:     event.ReceivedAt,
	})
	d.cond.Signal()
}

func (d *dataPushWebhookDispatcher) close() {
	if d == nil {
		return
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	d.cancel()
	d.cond.Broadcast()
	d.mu.Unlock()
	d.wg.Wait()
}

func (d *dataPushWebhookDispatcher) run() {
	for {
		item, ok := d.dequeue()
		if !ok {
			return
		}
		d.deliver(item)
	}
}

func (d *dataPushWebhookDispatcher) dequeue() (webhookQueueItem, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for len(d.queue) == 0 && !d.closed {
		d.cond.Wait()
	}
	if d.closed {
		return webhookQueueItem{}, false
	}
	item := d.queue[0]
	d.queue[0] = webhookQueueItem{}
	d.queue = d.queue[1:]
	return item, true
}

func (d *dataPushWebhookDispatcher) deliver(item webhookQueueItem) {
	backoff := d.retryInitialBackoff
	if backoff <= 0 {
		backoff = webhookRetryInitialBackoff
	}
	maximum := d.retryMaximumBackoff
	if maximum < backoff {
		maximum = backoff
	}
	attempt := 1
	for {
		err := d.post(item)
		if err == nil {
			if attempt > 1 {
				d.logger.Info(
					"webhook delivery recovered",
					"url", d.url,
					"topic", item.topic,
					"from_peer_id", item.fromPeerID,
					"idempotency_key", item.idempotencyKey,
					"attempts", attempt,
				)
			}
			return
		}
		if d.ctx.Err() != nil {
			return
		}
		d.logger.Warn(
			"webhook delivery failed; retrying",
			"url", d.url,
			"topic", item.topic,
			"from_peer_id", item.fromPeerID,
			"idempotency_key", item.idempotencyKey,
			"received_at", item.receivedAt,
			"attempt", attempt,
			"backoff", backoff,
			"err", err,
		)
		timer := time.NewTimer(backoff)
		select {
		case <-d.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		attempt++
		backoff = nextWebhookBackoff(backoff, maximum)
	}
}

func (d *dataPushWebhookDispatcher) post(item webhookQueueItem) error {
	req, err := http.NewRequestWithContext(d.ctx, http.MethodPost, d.url, bytes.NewReader(item.body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "aqua-webhook/1")
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, webhookResponseBodyMaxBytes))
	if len(body) == 0 {
		return fmt.Errorf("unexpected webhook status %d", resp.StatusCode)
	}
	return fmt.Errorf("unexpected webhook status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

func nextWebhookBackoff(current time.Duration, maximum time.Duration) time.Duration {
	if current <= 0 {
		current = webhookRetryInitialBackoff
	}
	if maximum <= 0 {
		maximum = webhookRetryMaximumBackoff
	}
	if current >= maximum {
		return maximum
	}
	next := current * 2
	if next > maximum {
		return maximum
	}
	return next
}
