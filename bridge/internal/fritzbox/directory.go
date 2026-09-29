package fritzbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

// Source delivers raw phonebook and call list data (implemented by Client).
type Source interface {
	Phonebook(ctx context.Context) ([]protocol.Contact, error)
	History(ctx context.Context, max int, loc *time.Location) ([]protocol.HistoryCall, error)
}

// Limits of GET /v1/history (spec v1.2).
const (
	HistoryDefaultLimit = 100
	HistoryMaxLimit     = 500
)

// DirectoryConfig configures the cache in front of the FRITZ!Box.
type DirectoryConfig struct {
	Source Source
	// Location converts the local call list times (fritzbox.timezone).
	Location *time.Location
	// CountryCode, e.g. "49", for matching caller numbers.
	CountryCode  string
	PhonebookTTL time.Duration
	HistoryTTL   time.Duration
	// FetchTimeout bounds one refresh against the FRITZ!Box.
	FetchTimeout time.Duration
	Now          func() time.Time
	Logger       *slog.Logger
}

// Directory caches the FRITZ!Box phonebook (10 min) and call list (30 s)
// so many devices cost the FRITZ!Box little. Concurrent refreshes are
// collapsed into one.
type Directory struct {
	cfg DirectoryConfig
	log *slog.Logger

	mu        sync.Mutex
	phonebook phonebookCache
	history   historyCache
	pbFlight  *flight
	hiFlight  *flight
}

type phonebookCache struct {
	fetchedAt time.Time
	body      []byte
	etag      string
	ok        bool
	tried     bool
	// names maps national numbers to contact names; "" marks numbers that
	// belong to more than one name. Kept after failed refreshes.
	names map[string]string
}

type historyCache struct {
	fetchedAt time.Time
	calls     []protocol.HistoryCall
	ok        bool
	tried     bool
}

type flight struct {
	done chan struct{}
	err  error
}

func (f *flight) wait(ctx context.Context) error {
	select {
	case <-f.done:
		return f.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// NewDirectory creates the cache. Nothing is fetched before Warmup or the
// first request.
func NewDirectory(cfg DirectoryConfig) *Directory {
	if cfg.PhonebookTTL == 0 {
		cfg.PhonebookTTL = 10 * time.Minute
	}
	if cfg.HistoryTTL == 0 {
		cfg.HistoryTTL = 30 * time.Second
	}
	if cfg.FetchTimeout == 0 {
		cfg.FetchTimeout = 30 * time.Second
	}
	if cfg.Location == nil {
		cfg.Location = time.UTC
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Directory{cfg: cfg, log: cfg.Logger.With("component", "fritzbox")}
}

// Warmup fetches both lists once, so welcome.features and caller names are
// available from the first call on.
func (d *Directory) Warmup(ctx context.Context) {
	if err := d.refreshPhonebook(ctx); err == nil {
		d.mu.Lock()
		count := len(d.phonebook.names)
		d.mu.Unlock()
		d.log.Info("FRITZ!Box phonebook loaded", "numbers", count)
	}
	if err := d.refreshHistory(ctx); err == nil {
		d.log.Info("FRITZ!Box call list loaded")
	}
}

// Features returns the v1.2 features whose last fetch succeeded.
func (d *Directory) Features() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var features []string
	if d.phonebook.ok {
		features = append(features, protocol.FeatureFritzBoxPhonebook)
	}
	if d.history.ok {
		features = append(features, protocol.FeatureFritzBoxHistory)
	}
	return features
}

// Phonebook returns the JSON body of GET /v1/phonebook and its ETag.
func (d *Directory) Phonebook(ctx context.Context) ([]byte, string, error) {
	d.mu.Lock()
	if d.phonebook.ok && d.cfg.Now().Sub(d.phonebook.fetchedAt) < d.cfg.PhonebookTTL {
		body, etag := d.phonebook.body, d.phonebook.etag
		d.mu.Unlock()
		return body, etag, nil
	}
	d.mu.Unlock()
	if err := d.refreshPhonebook(ctx); err != nil {
		return nil, "", err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.phonebook.body, d.phonebook.etag, nil
}

// History returns the JSON body of GET /v1/history for up to limit calls.
func (d *Directory) History(ctx context.Context, limit int) ([]byte, error) {
	limit = min(max(limit, 1), HistoryMaxLimit)
	d.mu.Lock()
	fresh := d.history.ok && d.cfg.Now().Sub(d.history.fetchedAt) < d.cfg.HistoryTTL
	d.mu.Unlock()
	if !fresh {
		if err := d.refreshHistory(ctx); err != nil {
			return nil, err
		}
	}
	d.mu.Lock()
	calls, fetchedAt := d.history.calls, d.history.fetchedAt
	d.mu.Unlock()
	if len(calls) > limit {
		calls = calls[:limit]
	}
	if calls == nil {
		calls = []protocol.HistoryCall{}
	}
	return json.Marshal(protocol.History{UpdatedAt: protocol.Timestamp(fetchedAt), Calls: calls})
}

// CallerName looks a caller number up in the cached phonebook. It never
// waits for the FRITZ!Box: without cache (or for ambiguous numbers) it
// returns "". A stale cache is refreshed in the background.
func (d *Directory) CallerName(number string) string {
	key := nationalNumber(number, d.cfg.CountryCode)
	if key == "" {
		return ""
	}
	d.mu.Lock()
	name := d.phonebook.names[key]
	stale := !d.phonebook.ok || d.cfg.Now().Sub(d.phonebook.fetchedAt) >= d.cfg.PhonebookTTL
	refresh := stale && d.pbFlight == nil
	d.mu.Unlock()
	if refresh {
		go func() { _ = d.refreshPhonebook(context.Background()) }()
	}
	return name
}

func (d *Directory) refreshPhonebook(ctx context.Context) error {
	d.mu.Lock()
	if f := d.pbFlight; f != nil {
		d.mu.Unlock()
		return f.wait(ctx)
	}
	f := &flight{done: make(chan struct{})}
	d.pbFlight = f
	d.mu.Unlock()

	// Detached from the first caller so its cancellation doesn't fail the
	// others waiting on the same flight.
	fetchCtx, cancel := context.WithTimeout(context.Background(), d.cfg.FetchTimeout)
	contacts, err := d.cfg.Source.Phonebook(fetchCtx)
	cancel()

	var body []byte
	var etag string
	if err == nil {
		body, etag, err = phonebookBody(contacts, d.cfg.Now())
	}

	d.mu.Lock()
	wasOK, tried := d.phonebook.ok, d.phonebook.tried
	d.phonebook.tried = true
	if err == nil {
		d.phonebook.ok = true
		d.phonebook.fetchedAt = d.cfg.Now()
		d.phonebook.body, d.phonebook.etag = body, etag
		d.phonebook.names = nameIndex(contacts, d.cfg.CountryCode)
	} else {
		d.phonebook.ok = false
	}
	d.pbFlight = nil
	d.mu.Unlock()

	if err != nil && (wasOK || !tried) {
		d.log.Warn("FRITZ!Box phonebook unavailable", "error", err, "hint", userMessage(err))
	}
	f.err = err
	close(f.done)
	return err
}

func (d *Directory) refreshHistory(ctx context.Context) error {
	d.mu.Lock()
	if f := d.hiFlight; f != nil {
		d.mu.Unlock()
		return f.wait(ctx)
	}
	f := &flight{done: make(chan struct{})}
	d.hiFlight = f
	d.mu.Unlock()

	fetchCtx, cancel := context.WithTimeout(context.Background(), d.cfg.FetchTimeout)
	calls, err := d.cfg.Source.History(fetchCtx, HistoryMaxLimit, d.cfg.Location)
	cancel()

	d.mu.Lock()
	wasOK, tried := d.history.ok, d.history.tried
	d.history.tried = true
	if err == nil {
		d.history = historyCache{fetchedAt: d.cfg.Now(), calls: calls, ok: true, tried: true}
	} else {
		d.history.ok = false
	}
	d.hiFlight = nil
	d.mu.Unlock()

	if err != nil && (wasOK || !tried) {
		d.log.Warn("FRITZ!Box call list unavailable", "error", err, "hint", userMessage(err))
	}
	f.err = err
	close(f.done)
	return err
}

// phonebookBody encodes the response; the ETag covers only the contacts,
// not updatedAt, so it changes exactly when the content does.
func phonebookBody(contacts []protocol.Contact, now time.Time) ([]byte, string, error) {
	if contacts == nil {
		contacts = []protocol.Contact{}
	}
	content, err := json.Marshal(contacts)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(content)
	body, err := json.Marshal(protocol.Phonebook{UpdatedAt: protocol.Timestamp(now), Contacts: contacts})
	if err != nil {
		return nil, "", err
	}
	return body, `"` + hex.EncodeToString(sum[:16]) + `"`, nil
}

// nameIndex maps every number to its contact name; numbers shared by
// different names map to "" (ambiguous: no name is better than a wrong one).
func nameIndex(contacts []protocol.Contact, countryCode string) map[string]string {
	names := map[string]string{}
	for _, c := range contacts {
		for _, n := range c.Numbers {
			key := nationalNumber(n.Number, countryCode)
			if key == "" {
				continue
			}
			if existing, seen := names[key]; seen && existing != c.Name {
				names[key] = ""
				continue
			}
			names[key] = c.Name
		}
	}
	return names
}

// userMessage returns the German explanation of a FRITZ!Box error.
func userMessage(err error) string {
	var fe *Error
	if errors.As(err, &fe) {
		return fe.UserMessage()
	}
	return (&Error{Kind: KindUnreachable}).UserMessage()
}
