package chaos

import (
	"fmt"
	stdnet "net"
	"sync"
	"time"

	riftnet "github.com/anshkanyadi/rift/net"
	"github.com/anshkanyadi/rift/net/tcp"
	"github.com/anshkanyadi/rift/sim"
)

// WireClient drives real operations at a real cluster over a real socket, and
// records what came back into the Client that owns the history.
//
// # It broadcasts, because the sim's dispatch does
//
// Every request goes to EVERY node and only the leader acts on it. That is the
// sim sweep's dispatch, reproduced here so the real run exercises the path the
// seeded runs exercised rather than a different one wearing the same name.
//
// It also means the two-responses case is not hypothetical. A deposed leader
// that has not learned it yet, a leader elected mid-request, a duplicate off a
// reordering wire: each puts a second answer on the wire for one operation, and
// each is classified by Client.Correlate rather than dropped.
//
//	AN OPERATION ADDRESSED TO ONE NODE WOULD HAVE MADE FOUR FIFTHS OF THE RUN
//	TIMEOUTS AND THE CORRELATION MACHINERY UNREACHABLE -- a mechanism declared
//	and never invoked, which is the class this project keeps counting.
type WireClient struct {
	id  sim.NodeID
	tr  *tcp.Transport
	rec *Client
	ln  stdnet.Listener

	nodes []sim.NodeID

	mu       sync.Mutex
	inflight map[uint64]time.Time
	timeout  time.Duration
}

// NewWireClient listens on addr and talks to nodes.
func NewWireClient(id sim.NodeID, addr string, nodes map[sim.NodeID]string, rec *Client, timeout time.Duration) (*WireClient, error) {
	ids := make([]sim.NodeID, 0, len(nodes))
	for n := range nodes {
		ids = append(ids, n)
	}
	// Sorted: the order requests go out in is behaviour, and behaviour that
	// varies run to run for no reason is behaviour nobody can debug.
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
	w := &WireClient{
		id: id, tr: tcp.New(id, nodes), rec: rec, nodes: ids,
		inflight: map[uint64]time.Time{}, timeout: timeout,
	}
	ln, err := tcp.Listen(addr, w.onEnvelope)
	if err != nil {
		w.tr.Close()
		return nil, err
	}
	w.ln = ln
	return w, nil
}

// Close stops the client.
func (w *WireClient) Close() {
	if w.ln != nil {
		w.ln.Close()
	}
	w.tr.Close()
}

// Do issues one operation and returns once it has an outcome or has timed out.
//
// It does NOT wait for every node to answer. The first response decides, and
// the rest arrive whenever they arrive -- into Correlate, which is where the
// second answer to one request is either wire weather or a finding.
func (w *WireClient) Do(op, key, value string, wait time.Duration) bool {
	var wireOp byte = riftnet.OpGet
	if op == "put" {
		wireOp = riftnet.OpPut
	}
	_, seq := w.rec.BeginSeq(op, key, value)
	body, err := riftnet.EncodeRequest(riftnet.Request{Op: wireOp, Seq: seq, Key: key, Value: value})
	if err != nil {
		// An operation this client could not even encode never reached the
		// cluster. It is still ENDED, as an error, because an operation left
		// open in the history is a claim that it might have taken effect.
		w.rec.Correlate(seq, sim.RespError, "")
		return false
	}
	w.mu.Lock()
	w.inflight[seq] = time.Now()
	w.mu.Unlock()

	for _, n := range w.nodes {
		w.tr.Send(sim.Envelope{From: w.id, To: n, Kind: riftnet.KindRequest, Body: body})
	}

	// Poll for the outcome. A condition variable would be tidier; a poll is
	// what makes the TIMEOUT observable at a bounded delay even when no
	// response ever arrives, which is the case the run exists to produce.
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		w.mu.Lock()
		_, still := w.inflight[seq]
		w.mu.Unlock()
		if !still {
			return true
		}
		time.Sleep(200 * time.Microsecond)
	}
	w.mu.Lock()
	_, still := w.inflight[seq]
	if still {
		delete(w.inflight, seq)
	}
	w.mu.Unlock()
	if still {
		// THE OPERATION STAYS IN THE HISTORY. A partitioned cluster that stops
		// answering is behaving correctly; the operation is the one that might
		// have taken effect invisibly, and dropping it makes the history
		// smaller, cleaner and wrong.
		w.rec.Timeout(seq)
	}
	return !still
}

// onEnvelope handles one response off the wire.
func (w *WireClient) onEnvelope(e sim.Envelope) {
	if e.Kind != riftnet.KindResponse {
		// Not a response. Counted nowhere and answered nowhere: this client
		// speaks one protocol and anything else on its socket is somebody
		// else's traffic, which is a fact about the addressing rather than
		// about an operation.
		return
	}
	resp, err := riftnet.DecodeResponse(e.Body)
	if err != nil {
		// A RESPONSE THIS CLIENT COULD NOT PARSE IS NOT NOTHING. It cannot be
		// correlated -- the seq is exactly what was unreadable -- so it is
		// counted as unissued, which is the loud bucket, because a node putting
		// unparseable bytes on the response wire is a defect and not weather.
		w.rec.Correlate(^uint64(0), sim.RespError, "")
		return
	}
	kind := sim.RespError
	switch resp.Status {
	case riftnet.StatusOK:
		kind = sim.RespOK
	case riftnet.StatusNotFound:
		kind = sim.RespOK // a get that found nothing is an answer: the empty value
	}
	out := w.rec.Correlate(resp.Seq, kind, resp.Value)
	if out == Matched {
		w.mu.Lock()
		delete(w.inflight, resp.Seq)
		w.mu.Unlock()
	}
}

// Counters reports the client transport's own view.
func (w *WireClient) Counters() (sent, dropped, wireBytes uint64) { return w.tr.Counters() }

// Addr is where this client listens.
func (w *WireClient) Addr() string { return w.ln.Addr().String() }

func (w *WireClient) String() string { return fmt.Sprintf("client %d at %s", w.id, w.Addr()) }
