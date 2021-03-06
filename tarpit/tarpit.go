package tarpit

import (
	"container/list"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"time"
)

type Tarpit struct {
	contentType    string
	numTimeslices  int
	timeslice      time.Duration
	minResponseLen int64
	maxResponseLen int64
	rng            *rand.Rand
	toTimer        chan *tarpitConn
}

type tarpitConn struct {
	conn      net.Conn
	remaining int64
}

func New(workers int, contentType string, period, timeslice time.Duration, minResponseLen, maxResponseLen int64) *Tarpit {
	if workers <= 0 || contentType == "" || timeslice.Nanoseconds() <= 0 || period.Nanoseconds() < timeslice.Nanoseconds() || minResponseLen <= 0 || maxResponseLen < minResponseLen {
		return nil
	}

	t := &Tarpit{
		contentType:    contentType,
		numTimeslices:  (int(period) + int(timeslice) - 1) / int(timeslice),
		timeslice:      timeslice,
		minResponseLen: minResponseLen,
		maxResponseLen: maxResponseLen,
		rng:            rand.New(rand.NewSource(time.Now().UnixNano())),
		toTimer:        make(chan *tarpitConn, 10000),
	}

	for i := 0; i < workers; i++ {
		go t.timer()
	}

	return t
}

func (t *Tarpit) Handler(w http.ResponseWriter, r *http.Request) {
	responseLen := t.rng.Int63n(t.maxResponseLen-t.minResponseLen+1) + t.minResponseLen

	// Headers must reflect that we don't do chunked encoding.
	w.Header().Set("Content-Type", t.contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(responseLen, 10))
	w.WriteHeader(http.StatusOK)

	if conn, _, ok := hijack(w); ok {
		// Pass this connection on to tarpit.timer().
		tc := &tarpitConn{
			conn:      conn,
			remaining: responseLen,
		}
		t.toTimer <- tc
	}
}

func (t *Tarpit) Close() {
	close(t.toTimer)
}

func (t *Tarpit) timer() {
	timeslices := make([]*list.List, t.numTimeslices)
	for i := range timeslices {
		timeslices[i] = list.New()
	}

	// At startup, randomize within timeslice to try to avoid thundering herd.
	time.Sleep(time.Duration(t.rng.Int63n(int64(t.timeslice))))

	tick := time.NewTicker(t.timeslice)

	nextslice := 0

outer:
	for {
		select {
		case tc, ok := <-t.toTimer:
			if !ok {
				break outer
			}
			timeslices[t.rng.Intn(len(timeslices))].PushBack(tc)

		case <-tick.C:
			// Pick a printable ascii character to send.
			b := make([]byte, 1)
			b[0] = byte(t.rng.Int31n(95) + 32)

			writeConns(timeslices[nextslice], b)

			nextslice++
			if nextslice >= len(timeslices) {
				nextslice = 0
			}
		}
	}

	tick.Stop()

	for slice := 0; slice < len(timeslices); slice++ {
		closeConns(timeslices[slice])
	}
}

// Write a byte array to all conns in a timeslice.

func writeConns(conns *list.List, b []byte) {
	var en *list.Element
	for e := conns.Front(); e != nil; e = en {
		en = e.Next()

		tc, _ := e.Value.(*tarpitConn)

		// This theoretically could block.
		n, err := tc.conn.Write(b)

		tc.remaining--
		if tc.remaining <= 0 || n == 0 || err != nil {
			conns.Remove(e)
			_ = tc.conn.Close()
		}
	}
}

// Close all conns in a timeslice.

func closeConns(conns *list.List) {
	var en *list.Element
	for e := conns.Front(); e != nil; e = en {
		en = e.Next()

		tc, _ := e.Value.(*tarpitConn)
		conns.Remove(e)
		_ = tc.conn.Close()
	}
}
