// Package tracker provides support for announcing torrents to HTTP and UDP trackers.
package tracker

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"net/url"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/cenkalti/rain/logger"
)

// Number of peers we want from trackers
const numWant = 50

var requestCancelledErr = errors.New("request cancelled")

type Tracker interface {
	URL() string
	// Announce transfer to the tracker.
	Announce(t Transfer, e TrackerEvent, cancel <-chan struct{}) (*AnnounceResponse, error)
	Scrape([]*Transfer) (*scrapeResponse, error)
	// Close must be called in order to close open connections if Announce is ever called.
	Close() error
}

type AnnounceResponse struct {
	Error      error
	Interval   time.Duration
	Leechers   int32
	Seeders    int32
	Peers      []*net.TCPAddr
	ExternalIP net.IP
}

type scrapeResponse struct {
	// TODO not implemented
}

// AnnouncePeriodically announces to the tracker periodically and adjust the interval according to the response returned by the tracker.
// Puts responses into responseC. Blocks when sending to this channel.
// t.Close must be called after using this function to close open connections to the tracker.
func AnnouncePeriodically(t Tracker, transfer Transfer, cancel <-chan struct{}, StartEvent TrackerEvent, eventC <-chan TrackerEvent, responseC chan<- *AnnounceResponse) {
	var nextAnnounce time.Duration
	var retry = *defaultRetryBackoff

	announce := func(e TrackerEvent) {
		r, err := t.Announce(transfer, e, cancel)
		if err != nil {
			r = &AnnounceResponse{Error: err}
			nextAnnounce = retry.NextBackOff()
		} else {
			retry.Reset()
			nextAnnounce = r.Interval
		}
		select {
		case responseC <- r:
		case <-cancel:
			return
		}
	}

	announce(StartEvent)
	for {
		select {
		case <-time.After(nextAnnounce):
			announce(EventNone)
		case e := <-eventC:
			announce(e)
		case <-cancel:
			return
		}
	}
}

type TrackerBase struct {
	Url    *url.URL
	Rawurl string
	Client Client
	Log    logger.Logger
}

func (t TrackerBase) URL() string { return t.Rawurl }

// parsePeersBinary parses compact representation of peer list.
func (t *TrackerBase) parsePeersBinary(r *bytes.Reader) ([]*net.TCPAddr, error) {
	t.Log.Debugf("len(rest): %#v", r.Len())
	if r.Len()%6 != 0 {
		b := make([]byte, r.Len())
		_, _ = r.Read(b)
		t.Log.Debugf("Peers: %q", b)
		return nil, errors.New("invalid peer list")
	}

	count := r.Len() / 6
	t.Log.Debugf("count of peers: %#v", count)
	peers := make([]*net.TCPAddr, count)
	for i := 0; i < count; i++ {
		var peer struct {
			IP   [net.IPv4len]byte
			Port uint16
		}
		if err := binary.Read(r, binary.BigEndian, &peer); err != nil {
			return nil, err
		}
		peers[i] = &net.TCPAddr{IP: peer.IP[:], Port: int(peer.Port)}
	}

	t.Log.Debugf("peers: %#v\n", peers)
	return peers, nil
}

// Error is the string that is sent by the tracker from announce or scrape.
type trackerError string

func (e trackerError) Error() string { return string(e) }

type TrackerEvent int32

// Tracker Announce Events. Numbers corresponds to constants in UDP tracker protocol.
const (
	EventNone TrackerEvent = iota
	EventCompleted
	EventStarted
	EventStopped
)

var eventNames = [...]string{
	"empty",
	"completed",
	"started",
	"stopped",
}

// String returns the name of event as represented in HTTP tracker protocol.
func (e TrackerEvent) String() string {
	return eventNames[e]
}

// defaultRetryBackoff is the back-off algorithm to use before retrying failed announce and scrape operations.
var defaultRetryBackoff = &backoff.ExponentialBackOff{
	InitialInterval:     5 * time.Second,
	RandomizationFactor: 0.5,
	Multiplier:          2,
	MaxInterval:         30 * time.Minute,
	MaxElapsedTime:      0, // never stop
	Clock:               backoff.SystemClock,
}

func init() {
	defaultRetryBackoff.Reset()
}
