package gamequery

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeSLP accepts one connection, reads the handshake and status request, then runs reply.
func fakeSLP(t *testing.T, reply func(conn net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		for range 2 { // handshake, status request
			n, err := readVarInt(r)
			if err != nil {
				return
			}
			if _, err := r.Discard(int(n)); err != nil {
				return
			}
		}
		reply(conn)
	}()
	return ln.Addr().String()
}

func statusPacket(body string) []byte {
	var payload bytes.Buffer
	writeVarInt(&payload, 0x00)
	writeVarInt(&payload, int32(len(body)))
	payload.WriteString(body)
	var out bytes.Buffer
	writeVarInt(&out, int32(payload.Len()))
	out.Write(payload.Bytes())
	return out.Bytes()
}

func TestMinecraftReadsCountAndDropsFakeSampleEntries(t *testing.T) {
	status, _ := json.Marshal(map[string]any{
		"version": map[string]any{"name": "Paper 1.21", "protocol": 767},
		"players": map[string]any{
			"max": 20, "online": 3,
			"sample": []map[string]string{
				{"name": "Kevin", "id": "4566e69f-c907-48ee-8d71-d7ba5aa00d20"},
				{"name": "§aBem-vindo!", "id": "00000000-0000-0000-0000-000000000000"},
				{"name": "Alex", "id": "069a79f4-44e9-4726-a5be-fca90e38aaf5"},
			},
		},
	})
	addr := fakeSLP(t, func(c net.Conn) { c.Write(statusPacket(string(status))) })
	got, err := Minecraft(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	if got.Online != 3 || got.Max != 20 || strings.Join(got.Players, ",") != "Kevin,Alex" {
		t.Fatalf("got %+v", got)
	}
}

func TestMinecraftRejectsOversizedResponse(t *testing.T) {
	addr := fakeSLP(t, func(c net.Conn) {
		var b bytes.Buffer
		writeVarInt(&b, 2<<20) // 2 MiB announced
		c.Write(b.Bytes())
	})
	if _, err := Minecraft(context.Background(), addr); err == nil {
		t.Fatal("want error for a response over 1 MiB")
	}
}

func TestMinecraftTimesOut(t *testing.T) {
	addr := fakeSLP(t, func(c net.Conn) { time.Sleep(2 * time.Second) })
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := Minecraft(ctx, addr); err == nil {
		t.Fatal("want timeout error")
	}
	if time.Since(start) > time.Second {
		t.Fatal("did not honour the context deadline")
	}
}

func TestMinecraftCapsPlayerNames(t *testing.T) {
	var sample []map[string]string
	for i := range MaxPlayers + 10 {
		sample = append(sample, map[string]string{"name": "p" + string(rune('a'+i%26)), "id": "4566e69f-c907-48ee-8d71-d7ba5aa00d20"})
	}
	status, _ := json.Marshal(map[string]any{"players": map[string]any{"max": 100, "online": 60, "sample": sample}})
	addr := fakeSLP(t, func(c net.Conn) { c.Write(statusPacket(string(status))) })
	got, err := Minecraft(context.Background(), addr)
	if err != nil || len(got.Players) != MaxPlayers || got.Online != 60 {
		t.Fatalf("got %d names, online %d, err %v", len(got.Players), got.Online, err)
	}
}
