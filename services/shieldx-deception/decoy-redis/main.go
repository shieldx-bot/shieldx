package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"time"
)

func getenvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func main() {
	port := getenvInt("DECOY_REDIS_PORT", 6380)
	maxConns := getenvInt("DECOY_REDIS_MAX_CONNS", 500)
	if maxConns <= 0 { maxConns = 500 }
	sem := make(chan struct{}, maxConns)

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Printf("[decoy-redis] listening on :%d", port)
	banner := os.Getenv("DECOY_REDIS_BANNER")
	if banner == "" {
		banner = "+PONG\r\n"
	}
	for {
		c, err := ln.Accept()
		if err != nil {
			continue
		}
		select {
		case sem <- struct{}{}:
		default:
			_ = c.SetDeadline(time.Now().Add(100 * time.Millisecond))
			_ = c.Close()
			continue
		}
		go func(conn net.Conn) {
			defer func() { <-sem; _ = conn.Close() }()
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			r := bufio.NewReader(conn)
			// read and ignore one line, respond with banner
			_, _ = r.ReadString('\n')
			fmt.Fprintf(conn, "%s", banner)
		}(c)
	}
}
