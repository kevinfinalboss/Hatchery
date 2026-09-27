package gamequery

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
)

// maxStatusBytes bounds the status response. The favicon (base64 PNG) makes it large;
// anything over 1 MiB is not a legitimate status.
const maxStatusBytes = 1 << 20

// zeroUUID marks sample entries that are text (servers write MOTD lines there), not players.
const zeroUUID = "00000000-0000-0000-0000-000000000000"

type slpStatus struct {
	Players struct {
		Max    int `json:"max"`
		Online int `json:"online"`
		Sample []struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		} `json:"sample"`
	} `json:"players"`
}

// Minecraft runs the Server List Ping (1.7+): handshake with next state 1, status request,
// and reads the JSON status. It does not send the latency ping.
func Minecraft(ctx context.Context, addr string) (Result, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return Result{}, err
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return Result{}, fmt.Errorf("bad port %q", portStr)
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return Result{}, err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	var hs bytes.Buffer
	writeVarInt(&hs, 0x00) // packet id: handshake
	writeVarInt(&hs, -1)   // protocol version: "any" for status
	writeVarInt(&hs, int32(len(host)))
	hs.WriteString(host)
	_ = binary.Write(&hs, binary.BigEndian, uint16(port))
	writeVarInt(&hs, 1) // next state: status

	var out bytes.Buffer
	writeVarInt(&out, int32(hs.Len()))
	out.Write(hs.Bytes())
	out.Write([]byte{0x01, 0x00}) // status request: length 1, packet id 0
	if _, err := conn.Write(out.Bytes()); err != nil {
		return Result{}, err
	}

	r := bufio.NewReader(conn)
	length, err := readVarInt(r)
	if err != nil {
		return Result{}, err
	}
	if length <= 0 || length > maxStatusBytes {
		return Result{}, fmt.Errorf("status response of %d bytes", length)
	}
	br := bufio.NewReader(io.LimitReader(r, int64(length)))
	if id, err := readVarInt(br); err != nil || id != 0x00 {
		return Result{}, errors.New("unexpected status packet")
	}
	n, err := readVarInt(br)
	if err != nil || n < 0 || n > maxStatusBytes {
		return Result{}, errors.New("bad status string length")
	}
	raw := make([]byte, n)
	if _, err := io.ReadFull(br, raw); err != nil {
		return Result{}, err
	}
	var st slpStatus
	if err := json.Unmarshal(raw, &st); err != nil {
		return Result{}, fmt.Errorf("decoding status: %w", err)
	}
	res := Result{Online: st.Players.Online, Max: st.Players.Max}
	for _, p := range st.Players.Sample {
		if p.ID == zeroUUID {
			continue
		}
		res.Players = appendName(res.Players, p.Name)
	}
	return res, nil
}

func writeVarInt(w *bytes.Buffer, v int32) {
	u := uint32(v)
	for {
		if u&^0x7F == 0 {
			w.WriteByte(byte(u))
			return
		}
		w.WriteByte(byte(u&0x7F | 0x80))
		u >>= 7
	}
}

func readVarInt(r io.ByteReader) (int32, error) {
	var v uint32
	for i := range 5 {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		v |= uint32(b&0x7F) << (7 * i)
		if b&0x80 == 0 {
			return int32(v), nil
		}
	}
	return 0, errors.New("varint too long")
}
