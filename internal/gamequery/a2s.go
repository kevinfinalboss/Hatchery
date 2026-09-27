package gamequery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
)

var (
	a2sHeader   = []byte{0xFF, 0xFF, 0xFF, 0xFF}
	a2sSplit    = []byte{0xFE, 0xFF, 0xFF, 0xFF}
	errA2SSplit = errors.New("split A2S response (not reassembled)")
)

const (
	a2sChallenge   = 0x41
	a2sInfoReply   = 0x49
	a2sPlayerReply = 0x44
)

// A2S asks A2S_INFO for the counts, then A2S_PLAYER for the names. A failed or split
// A2S_PLAYER still returns the counts; only a failed A2S_INFO is an error.
func A2S(ctx context.Context, addr string) (Result, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return Result{}, err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	info, err := a2sExchange(conn, func(ch []byte) []byte {
		b := append(append([]byte{}, a2sHeader...), 0x54)
		b = append(b, "Source Engine Query\x00"...)
		return append(b, ch...)
	}, a2sInfoReply)
	if err != nil {
		return Result{}, fmt.Errorf("A2S_INFO: %w", err)
	}
	res, err := parseA2SInfo(info)
	if err != nil {
		return Result{}, err
	}

	players, err := a2sExchange(conn, func(ch []byte) []byte {
		if ch == nil {
			ch = a2sHeader // "give me a challenge"
		}
		return append(append(append([]byte{}, a2sHeader...), 0x55), ch...)
	}, a2sPlayerReply)
	if err == nil {
		res.Players = parseA2SPlayers(players)
	}
	return res, nil
}

// a2sExchange sends build(nil); on a 0x41 challenge it resends build(challenge). It returns
// the reply body after the type byte.
func a2sExchange(conn net.Conn, build func(challenge []byte) []byte, want byte) ([]byte, error) {
	var challenge []byte
	buf := make([]byte, 64<<10)
	for range 2 {
		if _, err := conn.Write(build(challenge)); err != nil {
			return nil, err
		}
		n, err := conn.Read(buf)
		if err != nil {
			return nil, err
		}
		reply := buf[:n]
		if bytes.HasPrefix(reply, a2sSplit) {
			return nil, errA2SSplit
		}
		if len(reply) < 5 || !bytes.HasPrefix(reply, a2sHeader) {
			return nil, errors.New("malformed reply")
		}
		switch reply[4] {
		case a2sChallenge:
			if len(reply) < 9 {
				return nil, errors.New("short challenge")
			}
			challenge = append([]byte{}, reply[5:9]...)
		case want:
			return append([]byte{}, reply[5:]...), nil
		default:
			return nil, fmt.Errorf("unexpected reply type 0x%02x", reply[4])
		}
	}
	return nil, errors.New("challenged twice")
}

type a2sReader struct {
	b   []byte
	err error
}

func (r *a2sReader) byte() byte {
	if r.err != nil || len(r.b) < 1 {
		r.err = errors.New("short A2S reply")
		return 0
	}
	v := r.b[0]
	r.b = r.b[1:]
	return v
}

func (r *a2sReader) skip(n int) {
	if r.err != nil || len(r.b) < n {
		r.err = errors.New("short A2S reply")
		return
	}
	r.b = r.b[n:]
}

func (r *a2sReader) cstring() string {
	if r.err != nil {
		return ""
	}
	i := bytes.IndexByte(r.b, 0)
	if i < 0 {
		r.err = errors.New("unterminated string")
		return ""
	}
	s := string(r.b[:i])
	r.b = r.b[i+1:]
	return s
}

func parseA2SInfo(body []byte) (Result, error) {
	r := &a2sReader{b: body}
	r.byte()    // protocol
	r.cstring() // name
	r.cstring() // map
	r.cstring() // folder
	r.cstring() // game
	r.skip(2)   // app id
	players, max := r.byte(), r.byte()
	if r.err != nil {
		return Result{}, r.err
	}
	return Result{Online: int(players), Max: int(max)}, nil
}

func parseA2SPlayers(body []byte) []string {
	r := &a2sReader{b: body}
	count := int(r.byte())
	var names []string
	for range count {
		r.byte() // index
		name := r.cstring()
		r.skip(8) // score int32 + duration float32
		if r.err != nil {
			break
		}
		names = appendName(names, name)
	}
	return names
}
