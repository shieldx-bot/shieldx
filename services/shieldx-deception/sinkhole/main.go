package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
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
	tcpPort := getenvInt("SINKHOLE_TCP_PORT", 9095)
	udpPort := getenvInt("SINKHOLE_UDP_PORT", 9096)
	httpPort := getenvInt("SINKHOLE_HTTP_PORT", 9097)

	// TCP sinkhole
	go func() {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", tcpPort))
		if err != nil {
			log.Printf("tcp listen: %v", err)
			return
		}
		log.Printf("[sinkhole] TCP :%d", tcpPort)
		for {
			c, err := ln.Accept()
			if err != nil {
				continue
			}
			go func(conn net.Conn) { defer conn.Close(); io.Copy(io.Discard, conn) }(c)
		}
	}()
	// UDP sinkhole
	go func() {
		addr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", udpPort))
		c, err := net.ListenUDP("udp", addr)
		if err != nil {
			log.Printf("udp listen: %v", err)
			return
		}
		log.Printf("[sinkhole] UDP :%d", udpPort)
		buf := make([]byte, 65535)
		for {
			_, _, err := c.ReadFromUDP(buf)
			if err != nil {
				// keep the sinkhole alive under transient errors
				continue
			}
		}
	}()
	// HTTP sinkhole
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Discard body with a sane cap and respond 204 quickly
		_, _ = io.CopyN(io.Discard, r.Body, 1<<20)
		r.Body.Close()
		w.WriteHeader(204)
	})
	addr := fmt.Sprintf(":%d", httpPort)
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       3 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       15 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	log.Printf("[sinkhole] HTTP :%s", addr)
	log.Fatal(srv.ListenAndServe())
}
