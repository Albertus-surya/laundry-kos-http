package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	timeoutKlien   = 2 * time.Second        // wajib 1-3 detik
	maksPercobaan  = 5                      // wajib 3-5 percobaan
	backoffAwal    = 200 * time.Millisecond // jeda awal, lalu berlipat dua
	maksBody       = 1 << 20
	headerRetryAft = "Retry-After"
)

// httpClient adalah satu-satunya http.Client yang dipakai seluruh program.
var httpClient = &http.Client{Timeout: timeoutKlien}

const bantuan = `Pemakaian:
  go run ./klien <alamat> lihat <id>
  go run ./klien <alamat> buat <nota> <pelanggan>

Contoh:
  go run ./klien http://localhost:8080 lihat 1
  go run ./klien http://localhost:8080 buat N-0042 Andi`

// hasil adalah ringkasan satu response HTTP.
type hasil struct {
	status     int
	body       []byte
	retryAfter time.Duration // 0 bila header tidak ada atau tidak valid
}

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, bantuan)
		os.Exit(1)
	}

	alamat := normalisasiAlamat(os.Args[1])
	perintah := os.Args[2]
	args := os.Args[3:]

	var err error
	switch perintah {
	case "lihat":
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, bantuan)
			os.Exit(1)
		}
		err = lihat(alamat, args[0])
	case "buat":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, bantuan)
			os.Exit(1)
		}
		// Argumen ke-2 dan seterusnya digabung sebagai nama pelanggan.
		err = buat(alamat, args[0], strings.Join(args[1:], " "))
	default:
		fmt.Fprintf(os.Stderr, "perintah tidak dikenal: %q\n\n%s\n", perintah, bantuan)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "GAGAL:", err)
		os.Exit(1)
	}
}

func normalisasiAlamat(a string) string {
	if !strings.HasPrefix(a, "http://") && !strings.HasPrefix(a, "https://") {
		a = "http://" + a
	}
	return strings.TrimRight(a, "/")
}

// layakDiulang menentukan apakah status HTTP termasuk kegagalan sementara.
func layakDiulang(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// lihat mengambil satu cucian dengan retry exponential backoff.
func lihat(alamat, id string) error {
	target := alamat + "/cucian/" + url.PathEscape(id)

	var galatTerakhir error
	jeda := backoffAwal
	for percobaan := 1; percobaan <= maksPercobaan; percobaan++ {
		h, err := kirim(http.MethodGet, target, nil)
		switch {
		case err != nil:
			galatTerakhir = err // galat jaringan: timeout, koneksi putus, dsb.
		case layakDiulang(h.status):
			galatTerakhir = fmt.Errorf("server membalas status %d", h.status)
			if h.retryAfter > 0 {
				jeda = h.retryAfter // Retry-After mengalahkan backoff
			}
		case h.status != http.StatusOK:
			return galatDariServer(h.status, h.body) // 4xx lain tidak diulang
		default:
			cetakApaAdanya(h.body)
			return nil
		}

		if percobaan == maksPercobaan {
			break
		}
		fmt.Fprintf(os.Stderr, "percobaan ke-%d gagal (%v); mengulang dalam %v\n",
			percobaan, galatTerakhir, jeda)
		time.Sleep(jeda)
		jeda *= 2 // exponential backoff: 200ms, 400ms, 800ms, 1600ms
	}
	return fmt.Errorf("menyerah setelah %d percobaan: %w", maksPercobaan, galatTerakhir)
}

// buat mengirim POST tepat satu kali. Tidak ada retry agar tidak membuat data ganda.
func buat(alamat, nota, pelanggan string) error {
	payload, err := json.Marshal(map[string]string{"nota": nota, "pelanggan": pelanggan})
	if err != nil {
		return err
	}
	h, err := kirim(http.MethodPost, alamat+"/cucian", payload)
	if err != nil {
		return fmt.Errorf("permintaan gagal (tidak diulang otomatis agar tidak terjadi duplikasi): %w", err)
	}
	if h.status != http.StatusCreated && h.status != http.StatusOK {
		return galatDariServer(h.status, h.body)
	}
	cetakApaAdanya(h.body)
	return nil
}

// kirim melakukan satu request HTTP dan mengembalikan status, isi body, dan Retry-After.
func kirim(metode, target string, payload []byte) (hasil, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(metode, target, reader)
	if err != nil {
		return hasil{}, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return hasil{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maksBody))
	if err != nil {
		return hasil{}, err
	}
	return hasil{
		status:     resp.StatusCode,
		body:       body,
		retryAfter: bacaRetryAfter(resp.Header.Get(headerRetryAft)),
	}, nil
}

// bacaRetryAfter membaca header Retry-After dalam detik; 0 bila kosong atau tidak valid.
func bacaRetryAfter(nilai string) time.Duration {
	detik, err := strconv.Atoi(strings.TrimSpace(nilai))
	if err != nil || detik <= 0 {
		return 0
	}
	return time.Duration(detik) * time.Second
}

// galatDariServer menyusun pesan galat dari response non-sukses.
func galatDariServer(status int, body []byte) error {
	var g struct {
		Galat string `json:"galat"`
	}
	if json.Unmarshal(body, &g) == nil && g.Galat != "" {
		return fmt.Errorf("status %d: %s", status, g.Galat)
	}
	return fmt.Errorf("status %d: %s", status, strings.TrimSpace(string(body)))
}

// cetakApaAdanya mencetak JSON yang diterima tanpa diubah.
func cetakApaAdanya(body []byte) {
	fmt.Println(strings.TrimRight(string(body), "\r\n"))
}
