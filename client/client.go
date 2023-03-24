package client

import (
	"errors"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/song940/shadowsocks-go/core"
	"github.com/song940/shadowsocks-go/socks"
)

type LocalServer struct {
	remote string
	ciph   core.Cipher
}

func NewLocalServerFromURL(url string) (server *LocalServer) {
	addr, cipher, password, err := parseURL(url)
	if err != nil {
		log.Fatal(err)
	}
	var key []byte
	ciph, err := core.PickCipher(cipher, key, password)
	if err != nil {
		log.Fatal(err)
	}
	server = &LocalServer{addr, ciph}
	return
}

// Create a SOCKS server listening on addr and proxy to server.
func (server LocalServer) ListenAndServe(addr string) {
	log.Printf("SOCKS proxy %s <-> %s", addr, server.remote)
	tcpLocal(addr, server.remote, server.ciph.StreamConn, func(c net.Conn) (socks.Addr, error) {
		return socks.Handshake(c)
	})
}

func parseURL(s string) (addr, cipher, password string, err error) {
	u, err := url.Parse(s)
	if err != nil {
		return
	}

	addr = u.Host
	if u.User != nil {
		cipher = u.User.Username()
		password, _ = u.User.Password()
	}
	return
}

// Listen on addr and proxy to server to reach target from getAddr.
func tcpLocal(addr, server string, shadow func(net.Conn) net.Conn, getAddr func(net.Conn) (socks.Addr, error)) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("failed to listen on %s: %v", addr, err)
		return
	}

	for {
		c, err := l.Accept()
		if err != nil {
			log.Printf("failed to accept: %s", err)
			continue
		}

		go func() {
			defer c.Close()
			tgt, err := getAddr(c)
			if err != nil {

				// UDP: keep the connection until disconnect then free the UDP socket
				if err == socks.InfoUDPAssociate {
					buf := make([]byte, 1)
					// block here
					for {
						_, err := c.Read(buf)
						if err, ok := err.(net.Error); ok && err.Timeout() {
							continue
						}
						log.Println("UDP Associate End.")
						return
					}
				}

				log.Printf("failed to get target address: %v", err)
				return
			}

			rc, err := net.Dial("tcp", server)
			if err != nil {
				log.Printf("failed to connect to server %v: %v", server, err)
				return
			}
			defer rc.Close()
			rc = shadow(rc)

			if _, err = rc.Write(tgt); err != nil {
				log.Printf("failed to send target address: %v", err)
				return
			}

			log.Printf("proxy %s <-> %s <-> %s", c.RemoteAddr(), server, tgt)
			if err = relay(rc, c); err != nil {
				log.Printf("relay error: %v", err)
			}
		}()
	}
}

// relay copies between left and right bidirectionally
func relay(left, right net.Conn) error {
	var err, err1 error
	var wg sync.WaitGroup
	var wait = 5 * time.Second
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err1 = io.Copy(right, left)
		right.SetReadDeadline(time.Now().Add(wait)) // unblock read on right
	}()
	_, err = io.Copy(left, right)
	left.SetReadDeadline(time.Now().Add(wait)) // unblock read on left
	wg.Wait()
	if err1 != nil && !errors.Is(err1, os.ErrDeadlineExceeded) { // requires Go 1.15+
		return err1
	}
	if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
		return err
	}
	return nil
}
