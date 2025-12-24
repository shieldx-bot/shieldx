package main

import (
	"context"
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
	port := getenvInt("DECOY_SSH_PORT", 2222)
	// Concurrency guard to avoid resource exhaustion under heavy probing
	maxConns := getenvInt("DECOY_SSH_MAX_CONNS", 200)
	if maxConns <= 0 { maxConns = 200 }
	sem := make(chan struct{}, maxConns)

	// Use ListenConfig to set TCP keepalive for better resource cleanup
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Printf("[decoy-ssh] listening on :%d", port)
	banner := os.Getenv("DECOY_SSH_BANNER")
	if banner == "" {
		banner = "SSH-2.0-OpenSSH_8.2p1 Ubuntu-4ubuntu0.5"
	}
	for {
		c, err := ln.Accept()
		if err != nil {
			continue
		}
		// Acquire slot or immediately close if server at capacity
		select {
		case sem <- struct{}{}:
		default:
			_ = c.SetDeadline(time.Now().Add(100 * time.Millisecond))
			_ = c.Close()
			continue
		}
		go func(conn net.Conn) {
			defer func() { <-sem; _ = conn.Close() }()
			_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			// Write banner and keep the illusion for a short, bounded time
			fmt.Fprintf(conn, "%s\r\n", banner)
			time.Sleep(1500 * time.Millisecond)
		}(c)
	}
}
