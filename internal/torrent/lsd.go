package torrent

import (
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
	lsdReadTimeout   = 1 * time.Second
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

		buf := make([]byte, 2048)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			_ = recv.SetReadDeadline(time.Now().Add(lsdReadTimeout))
			n, from, err := recv.ReadFromUDP(buf)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				return
			}
			addr, ok := parseLSDAnnounce(buf[:n], from, encodedHash)
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

func parseLSDAnnounce(payload []byte, from *net.UDPAddr, expectedEncoded string) (net.TCPAddr, bool) {
	if from == nil || from.IP == nil || from.IP.IsUnspecified() {
		return net.TCPAddr{}, false
	}
	payloadStr := string(payload)
	lines := strings.Split(payloadStr, "\n")
	if len(lines) == 0 {
		return net.TCPAddr{}, false
	}
	start := strings.TrimSpace(lines[0])
	if !strings.HasPrefix(strings.ToUpper(start), "BT-SEARCH") {
		return net.TCPAddr{}, false
	}

	var parsedInfoHash string
	var port int

	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.IndexByte(line, ':')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])

		if strings.EqualFold(key, "infohash") {
			parsedInfoHash = val
		} else if strings.EqualFold(key, "port") {
			var err error
			port, err = strconv.Atoi(val)
			if err != nil || port <= 0 || port > 65535 {
				return net.TCPAddr{}, false
			}
		}
	}

	if !strings.EqualFold(parsedInfoHash, expectedEncoded) {
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
