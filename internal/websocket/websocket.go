package websocket

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	acceptMagic     = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	maxMessageBytes = 256 * 1024
	opcodeText      = 0x1
	opcodeClose     = 0x8
	opcodePing      = 0x9
	opcodePong      = 0xa
)

type Conn struct {
	conn        net.Conn
	reader      *bufio.Reader
	writeMasked bool
	writeMu     sync.Mutex
}

func Accept(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.Error(w, "websocket upgrade required", http.StatusUpgradeRequired)
		return nil, fmt.Errorf("websocket upgrade required")
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking unsupported", http.StatusInternalServerError)
		return nil, fmt.Errorf("hijacking unsupported")
	}
	netConn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}
	acceptRaw := sha1.Sum([]byte(key + acceptMagic))
	accept := base64.StdEncoding.EncodeToString(acceptRaw[:])
	if _, err := fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept); err != nil {
		_ = netConn.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		_ = netConn.Close()
		return nil, err
	}
	return &Conn{conn: netConn, reader: rw.Reader}, nil
}

func DialUnix(ctx context.Context, socketPath string, path string, token string, headers map[string]string) (*Conn, error) {
	dialer := net.Dialer{}
	netConn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, err
	}
	keyRaw := make([]byte, 16)
	if _, err := rand.Read(keyRaw); err != nil {
		_ = netConn.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyRaw)
	if deadline, ok := ctx.Deadline(); ok {
		_ = netConn.SetDeadline(deadline)
	}
	var builder strings.Builder
	builder.WriteString("GET ")
	builder.WriteString(path)
	builder.WriteString(" HTTP/1.1\r\nHost: podorel-agent\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: ")
	builder.WriteString(key)
	builder.WriteString("\r\n")
	if token != "" {
		builder.WriteString("Authorization: Bearer ")
		builder.WriteString(token)
		builder.WriteString("\r\n")
	}
	for name, value := range headers {
		if strings.TrimSpace(name) == "" || strings.ContainsAny(name, "\r\n:") || strings.ContainsAny(value, "\r\n") {
			continue
		}
		builder.WriteString(name)
		builder.WriteString(": ")
		builder.WriteString(value)
		builder.WriteString("\r\n")
	}
	builder.WriteString("\r\n")
	if _, err := io.WriteString(netConn, builder.String()); err != nil {
		_ = netConn.Close()
		return nil, err
	}
	reader := bufio.NewReader(netConn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		_ = netConn.Close()
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = netConn.Close()
		return nil, fmt.Errorf("agent websocket status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	_ = netConn.SetDeadline(time.Time{})
	return &Conn{conn: netConn, reader: reader, writeMasked: true}, nil
}

func (c *Conn) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.SetWriteDeadline(t)
}

func (c *Conn) WriteText(message string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return writeFrame(c.conn, opcodeText, []byte(message), c.writeMasked)
}

func (c *Conn) WriteClose() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return writeFrame(c.conn, opcodeClose, nil, c.writeMasked)
}

func (c *Conn) ReadText() (string, error) {
	for {
		opcode, payload, err := readFrame(c.reader)
		if err != nil {
			return "", err
		}
		switch opcode {
		case opcodeText:
			return string(payload), nil
		case opcodeClose:
			return "", io.EOF
		case opcodePing:
			_ = c.writeControl(opcodePong, payload)
		case opcodePong:
			continue
		default:
			return "", fmt.Errorf("unsupported websocket opcode %d", opcode)
		}
	}
}

func (c *Conn) writeControl(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return writeFrame(c.conn, opcode, payload, c.writeMasked)
}

func readFrame(r io.Reader) (byte, []byte, error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	opcode := header[0] & 0x0f
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7f)
	switch length {
	case 126:
		var extended [2]byte
		if _, err := io.ReadFull(r, extended[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(extended[:]))
	case 127:
		var extended [8]byte
		if _, err := io.ReadFull(r, extended[:]); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(extended[:])
	}
	if length > maxMessageBytes {
		return 0, nil, fmt.Errorf("websocket message exceeds %d bytes", maxMessageBytes)
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(r, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, int(length))
	if length > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return 0, nil, err
		}
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return opcode, payload, nil
}

func writeFrame(w io.Writer, opcode byte, payload []byte, masked bool) error {
	header := []byte{0x80 | opcode}
	length := len(payload)
	switch {
	case length < 126:
		header = append(header, byte(length))
	case length <= 65535:
		header = append(header, 126, byte(length>>8), byte(length))
	default:
		return fmt.Errorf("websocket message exceeds supported length")
	}
	if masked {
		header[1] |= 0x80
		mask := make([]byte, 4)
		if _, err := rand.Read(mask); err != nil {
			return err
		}
		header = append(header, mask...)
		maskedPayload := append([]byte(nil), payload...)
		for i := range maskedPayload {
			maskedPayload[i] ^= mask[i%4]
		}
		_, err := w.Write(append(header, maskedPayload...))
		return err
	}
	_, err := w.Write(append(header, payload...))
	return err
}
