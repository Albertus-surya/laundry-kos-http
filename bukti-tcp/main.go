// Command bukti-tcp mengirim raw HTTP request ke GET /healthz lewat TCP (net.Dial),
// tanpa memakai net/http.
//
// Pemakaian: go run ./bukti-tcp localhost:8080
package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Pemakaian: go run ./bukti-tcp localhost:8080")
		os.Exit(2)
	}
	host := os.Args[1]

	conn, err := net.Dial("tcp", host)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gagal terhubung:", err)
		os.Exit(1)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Request HTTP/1.1 mentah; setiap baris diakhiri CRLF dan header ditutup baris kosong.
	req := "GET /healthz HTTP/1.1\r\n" +
		"Host: " + host + "\r\n" +
		"Accept: application/json\r\n" +
		"Connection: close\r\n" +
		"\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		fmt.Fprintln(os.Stderr, "gagal mengirim request:", err)
		os.Exit(1)
	}

	// Connection: close membuat server menutup koneksi setelah membalas, sehingga ReadAll berhenti di EOF.
	resp, err := io.ReadAll(conn)
	if err != nil && len(resp) == 0 {
		fmt.Fprintln(os.Stderr, "gagal membaca respons:", err)
		os.Exit(1)
	}
	fmt.Println(strings.TrimSpace(string(resp)))
}
