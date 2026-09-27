package gamequery

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"
)

type fakeA2SOpts struct {
	challenge    bool // answer the first request of each kind with 0x41
	splitPlayers bool // answer A2S_PLAYER with a split packet header
	silent       bool // never answer
}

var testChallenge = []byte{0xDE, 0xAD, 0xBE, 0xEF}

func fakeA2S(t *testing.T, o fakeA2SOpts) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	challengeReply := append([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0x41}, testChallenge...)
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if o.silent || n < 5 {
				continue
			}
			req := buf[:n]
			hasChallenge := bytes.HasSuffix(req, testChallenge)
			switch req[4] {
			case 0x54:
				if o.challenge && !hasChallenge {
					pc.WriteTo(challengeReply, from)
					continue
				}
				pc.WriteTo(infoReply(3, 16), from)
			case 0x55:
				if !hasChallenge { // real servers always challenge A2S_PLAYER first
					pc.WriteTo(challengeReply, from)
					continue
				}
				if o.splitPlayers {
					pc.WriteTo([]byte{0xFE, 0xFF, 0xFF, 0xFF, 1, 2, 3, 4}, from)
					continue
				}
				pc.WriteTo(playerReply("Kevin", "", "Alex"), from)
			}
		}
	}()
	return pc.LocalAddr().String()
}

func cstr(b *bytes.Buffer, s string) { b.WriteString(s); b.WriteByte(0) }

func infoReply(players, max byte) []byte {
	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0x49, 17})
	cstr(&b, "Zomboid")
	cstr(&b, "Muldraugh, KY")
	cstr(&b, "zomboid")
	cstr(&b, "Project Zomboid")
	_ = binary.Write(&b, binary.LittleEndian, uint16(0))
	b.Write([]byte{players, max, 0, 'd', 'l', 0, 1})
	return b.Bytes()
}

func playerReply(names ...string) []byte {
	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0x44, byte(len(names))})
	for i, n := range names {
		b.WriteByte(byte(i))
		cstr(&b, n)
		_ = binary.Write(&b, binary.LittleEndian, int32(0))
		_ = binary.Write(&b, binary.LittleEndian, float32(12.5))
	}
	return b.Bytes()
}

func TestA2SWithoutInfoChallenge(t *testing.T) {
	got, err := A2S(context.Background(), fakeA2S(t, fakeA2SOpts{}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Online != 3 || got.Max != 16 || strings.Join(got.Players, ",") != "Kevin,Alex" {
		t.Fatalf("got %+v", got)
	}
}

func TestA2SWithChallenge(t *testing.T) {
	got, err := A2S(context.Background(), fakeA2S(t, fakeA2SOpts{challenge: true}))
	if err != nil || got.Online != 3 || len(got.Players) != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestA2SSplitPlayerReplyKeepsCount(t *testing.T) {
	got, err := A2S(context.Background(), fakeA2S(t, fakeA2SOpts{splitPlayers: true}))
	if err != nil || got.Online != 3 || got.Max != 16 || len(got.Players) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestA2SNoInfoAnswerIsError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := A2S(ctx, fakeA2S(t, fakeA2SOpts{silent: true})); err == nil {
		t.Fatal("want error")
	}
}
