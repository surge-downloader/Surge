package torrent

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	lsdMulticastAddr = "239.192.152.143"
	lsdPort          = 6771
	lsdAnnounceEvery = 30 * time.Second
)

func discoverLocalPeers(ctx context.Context, infoHash [20]byte, listenPort int) <-chan net.TCPAddr {
	out := make(chan net.TCPAddr, 128)

	go func() {
		defer close(out)

		group := net.ParseIP(lsdMulticastAddr)
		if group == nil {
			return
		}
		multicast := &net.UDPAddr{IP: group, Port: lsdPort}

		recv, err := net.ListenMulticastUDP("udp4", nil, multicast)
		if err != nil {
			return
		}
		defer func() { _ = recv.Close() }()
		_ = recv.SetReadBuffer(1 << 20)

		send, err := net.ListenUDP("udp4", nil)
		if err != nil {
			return
		}
		defer func() { _ = send.Close() }()

		if listenPort <= 0 || listenPort > 65535 {
			listenPort = 6881
		}
		encodedHash := percentEncodeInfoHash(infoHash)
		announce := makeLSDAnnounce(encodedHash, listenPort)
		sendAnnounce := func() {
			_, _ = send.WriteToUDP(announce, multicast)
		}

		sendAnnounce()
		go func() {
			ticker := time.NewTicker(lsdAnnounceEvery)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					sendAnnounce()
				}
			}
		}()

		go func() {
			<-ctx.Done()
			_ = recv.Close()
		}()

		buf := make([]byte, 2048)
		for {
			n, from, err := recv.ReadFromUDP(buf)
			if err != nil {
				return
			}
			addr, ok := parseLSDAnnounce(buf[:n], from, []byte(encodedHash))
			if !ok {
				continue
			}
			select {
			case out <- addr:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out
}

func makeLSDAnnounce(encodedHash string, listenPort int) []byte {
	if listenPort <= 0 || listenPort > 65535 {
		listenPort = 6881
	}
	msg := fmt.Sprintf(
		"BT-SEARCH * HTTP/1.1\r\nHost: %s:%d\r\nPort: %d\r\nInfohash: %s\r\n\r\n",
		lsdMulticastAddr,
		lsdPort,
		listenPort,
		encodedHash,
	)
	return []byte(msg)
}

func parseLSDAnnounce(payload []byte, from *net.UDPAddr, expectedEncoded []byte) (net.TCPAddr, bool) {
	if from == nil || from.IP == nil || from.IP.IsUnspecified() {
		return net.TCPAddr{}, false
	}
	lines := bytes.Split(payload, []byte{'\n'})
	if len(lines) == 0 {
		return net.TCPAddr{}, false
	}
	start := bytes.TrimSpace(lines[0])
	if !bytes.HasPrefix(bytes.ToUpper(start), []byte("BT-SEARCH")) {
		return net.TCPAddr{}, false
	}

	var parsedInfoHash []byte
	var port int

	for _, line := range lines[1:] {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		idx := bytes.IndexByte(line, ':')
		if idx < 0 {
			continue
		}
		key := bytes.TrimSpace(line[:idx])
		val := bytes.TrimSpace(line[idx+1:])

		if bytes.EqualFold(key, []byte("infohash")) {
			parsedInfoHash = val
		} else if bytes.EqualFold(key, []byte("port")) {
			var err error
			port, err = strconv.Atoi(string(val))
			if err != nil || port <= 0 || port > 65535 {
				return net.TCPAddr{}, false
			}
		}
	}

	if !bytes.EqualFold(parsedInfoHash, expectedEncoded) {
		return net.TCPAddr{}, false
	}
	if port <= 0 {
		return net.TCPAddr{}, false
	}

	ip := append(net.IP(nil), from.IP...)
	return net.TCPAddr{IP: ip, Port: port}, true
}

func percentEncodeInfoHash(infoHash [20]byte) string {
	var b strings.Builder
	b.Grow(len(infoHash) * 3)
	for _, v := range infoHash {
		fmt.Fprintf(&b, "%%%02X", v)
	}
	return b.String()
}
