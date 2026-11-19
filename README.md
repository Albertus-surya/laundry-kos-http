# Laundry Kos: REST API Pencatatan Cucian

## 1. Tema dan masalah

**Tema:** Laundry Kos.

Laundry dekat kos masih mencatat cucian di kertas. Nota hilang, cucian tertukar, dan pelanggan harus datang dulu hanya untuk tahu cuciannya sudah bisa diambil atau belum.

Proyek ini menyimpan setiap cucian dengan id unik dan status (`dicuci` atau `diambil`) sehingga bisa dicek lewat API kapan saja. Nota yang sama tidak bisa tercatat dua kali selama masih `dicuci`. Data disimpan di memori, dan hanya memakai standard library Go 1.22 atau lebih baru.

## 2. Cara menjalankan

Jalankan semua perintah dari folder yang berisi `go.mod`.

```bash
# Server (port dari env PORT, default 8080)
go run ./server
PORT=9000 go run ./server

# Klien (di terminal lain, server harus hidup)
go run ./klien http://localhost:8080 buat N-0042 Andi
go run ./klien http://localhost:8080 lihat 1

# Bukti HTTP di atas TCP
go run ./bukti-tcp localhost:8080

# Pemeriksaan kualitas
go vet ./...
gofmt -l .
```

## 3. Tabel endpoint

| Method dan path | Fungsi | Berhasil | Gagal |
|---|---|---|---|
| `POST /cucian` | Buat cucian baru (status `dicuci`) | 201 | 400 JSON rusak atau field kosong; 409 nota duplikat |
| `GET /cucian` | Semua cucian, urut id naik | 200 | |
| `GET /cucian?status=dicuci` | Filter menurut status (`dicuci` atau `diambil`) | 200 | 400 status tidak dikenal |
| `GET /cucian/{id}` | Satu cucian | 200 | 400 id bukan angka; 404 tidak ada |
| `POST /cucian/{id}/ambil` | Ubah `dicuci` menjadi `diambil` | 200 | 400; 404; 409 sudah `diambil` |
| `DELETE /cucian/{id}` | Hapus cucian berstatus `dicuci` | 200 | 400; 404; 409 sudah `diambil` |
| `GET /healthz` | Cek server hidup | 200 | |

Bentuk data: `{"id":1,"nota":"N-0042","pelanggan":"Andi","status":"dicuci"}`.
Bentuk daftar: `{"jumlah":1,"data":[...]}`. Bentuk error: `{"galat":"pesan"}`.

Contoh curl (data buatan):

```bash
# POST /cucian
curl -i -X POST http://localhost:8080/cucian -d '{"nota":"N-0042","pelanggan":"Andi"}'

# GET /cucian
curl -i http://localhost:8080/cucian

# GET /cucian?status=dicuci
curl -i "http://localhost:8080/cucian?status=dicuci"

# GET /cucian/{id}
curl -i http://localhost:8080/cucian/1

# POST /cucian/{id}/ambil
curl -i -X POST http://localhost:8080/cucian/1/ambil

# DELETE /cucian/{id}
curl -i -X DELETE http://localhost:8080/cucian/2

# GET /healthz
curl -i http://localhost:8080/healthz
```

Aturan data: id mulai dari 1 dan tidak dipakai ulang; spasi di awal dan akhir dibuang; duplikat nota dibandingkan tanpa membedakan huruf besar dan kecil dan hanya ditolak bila yang lama masih `dicuci`; field lain di JSON diabaikan; semua akses data dilindungi `sync.Mutex`.

## 4. Kebijakan retry

Hanya perintah `lihat` yang di-retry otomatis, bila terjadi error jaringan (timeout, koneksi ditolak), status 429, atau status 5xx.

- Paling banyak **5 percobaan** (1 percobaan awal dan 4 pengulangan).
- Jeda dimulai **200 ms** dan berlipat dua tiap pengulangan: 200, 400, 800, 1600 ms.
- Bila server mengirim header `Retry-After` (detik), nilainya dipakai sebagai jeda menggantikan backoff.
- Status 400 dan 404 tidak di-retry, karena jawabannya akan sama.
- Klien memakai satu `http.Client` dengan `Timeout` 2 detik.

Perintah `buat` **tidak pernah di-retry**. POST tidak idempotent: bila request pertama sudah sampai ke server tetapi balasannya hilang, pengulangan bisa membuat cucian ganda. Bila gagal, pengguna memeriksa dulu dengan `GET /cucian`.

## 5. Pemakaian AI

| Bagian | Alat | Keterangan |
|---|---|---|
| `server/`, `klien/`, `bukti-tcp/`, `go.mod`, dan draf README | Claude (Anthropic) | Menulis dan merevisi kode agar sesuai kontrak soal, lalu menguji dengan `go vet`, `gofmt`, curl, dan server nakal untuk klien. |
