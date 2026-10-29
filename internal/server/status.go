package server

import (
	"context"
	"encoding/json"
	"fmt"
	pk "github.com/Tnze/go-mc/net/packet"
	"modgrid.local/modgrid/internal/document"
	"net"
	"strconv"
	"time"
)

type Status struct {
	Version struct {
		Name     string `json:"name"`
		Protocol int    `json:"protocol"`
	} `json:"version"`
	Players struct {
		Max    int `json:"max"`
		Online int `json:"online"`
	} `json:"players"`
	Description json.RawMessage `json:"description"`
}

func Ping(ctx context.Context, address string) (Status, error) {
	var status Status
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return status, err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return status, document.Fail("UnsupportedNetwork", "status accepts an explicit loopback address")
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil || n == 0 {
		return status, document.Fail("InvalidInput", "invalid status port")
	}
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return status, err
	}
	defer conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return status, err
	}
	packet := pk.Marshal(0, pk.VarInt(-1), pk.String(host), pk.UnsignedShort(n), pk.VarInt(1))
	if err := packet.Pack(conn, -1); err != nil {
		return status, err
	}
	packet = pk.Marshal(0)
	if err := packet.Pack(conn, -1); err != nil {
		return status, err
	}
	if err := packet.UnPack(conn, -1); err != nil {
		return status, err
	}
	if packet.ID != 0 {
		return status, fmt.Errorf("unexpected status packet")
	}
	var raw pk.String
	if err := packet.Scan(&raw); err != nil {
		return status, err
	}
	if len(raw) > 65536 {
		return status, document.Fail("ResourceLimit", "status JSON exceeds 64 KiB")
	}
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		return status, err
	}
	if status.Version.Name == "" || len(status.Description) == 0 || status.Players.Max < 1 {
		return status, document.Fail("InvalidStatus", "missing status fields")
	}
	nonce := pk.Long(time.Now().UnixNano())
	packet = pk.Marshal(1, nonce)
	if err := packet.Pack(conn, -1); err != nil {
		return status, err
	}
	if err := packet.UnPack(conn, -1); err != nil {
		return status, err
	}
	var pong pk.Long
	if packet.ID != 1 {
		return status, document.Fail("InvalidStatus", "missing pong")
	}
	if err := packet.Scan(&pong); err != nil {
		return status, err
	}
	if pong != nonce {
		return status, document.Fail("InvalidStatus", "pong nonce mismatch")
	}
	return status, nil
}
