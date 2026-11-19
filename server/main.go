// Command server menjalankan REST API "Laundry Kos" dengan penyimpanan in-memory.
package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Nilai status cucian.
const (
	StatusDicuci  = "dicuci"
	StatusDiambil = "diambil"
)

// Galat domain yang diterjemahkan menjadi status HTTP oleh handler.
var (
	errTidakDitemukan  = errors.New("data tidak ditemukan")
	errDuplikat        = errors.New("data yang sama masih aktif")
	errSudahDiambil    = errors.New("data sudah diambil")
	errTidakBolehHapus = errors.New("data yang sudah diambil tidak boleh dihapus")
)

// Cucian adalah satu data cucian milik pelanggan.
type Cucian struct {
	ID        int    `json:"id"`
	Nota      string `json:"nota"`
	Pelanggan string `json:"pelanggan"`
	Status    string `json:"status"`
}

// penyimpanan menyimpan seluruh data di memori dan aman dipakai banyak goroutine.
type penyimpanan struct {
	mu     sync.Mutex
	nextID int
	data   []Cucian // selalu terurut naik berdasarkan id
}

func baruPenyimpanan() *penyimpanan {
	return &penyimpanan{nextID: 1, data: []Cucian{}}
}

// cari mengembalikan indeks data dengan id tertentu, atau -1. Pemanggil harus memegang kunci.
func (p *penyimpanan) cari(id int) int {
	for i := range p.data {
		if p.data[i].ID == id {
			return i
		}
	}
	return -1
}

func (p *penyimpanan) buat(nota, pelanggan string) (Cucian, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, c := range p.data {
		if c.Status == StatusDicuci && strings.EqualFold(c.Nota, nota) {
			return Cucian{}, errDuplikat
		}
	}
	c := Cucian{ID: p.nextID, Nota: nota, Pelanggan: pelanggan, Status: StatusDicuci}
	p.nextID++ // id tidak pernah dipakai ulang, meskipun data dihapus
	p.data = append(p.data, c)
	return c, nil
}

// semua mengembalikan salinan data; status kosong berarti tanpa filter.
func (p *penyimpanan) semua(status string) []Cucian {
	p.mu.Lock()
	defer p.mu.Unlock()

	hasil := make([]Cucian, 0, len(p.data)) // non-nil agar JSON menjadi [] bukan null
	for _, c := range p.data {
		if status == "" || c.Status == status {
			hasil = append(hasil, c)
		}
	}
	return hasil
}

func (p *penyimpanan) ambilSatu(id int) (Cucian, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	i := p.cari(id)
	if i < 0 {
		return Cucian{}, errTidakDitemukan
	}
	return p.data[i], nil
}

func (p *penyimpanan) tandaiDiambil(id int) (Cucian, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	i := p.cari(id)
	if i < 0 {
		return Cucian{}, errTidakDitemukan
	}
	if p.data[i].Status != StatusDicuci {
		return Cucian{}, errSudahDiambil
	}
	p.data[i].Status = StatusDiambil
	return p.data[i], nil
}

func (p *penyimpanan) hapus(id int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	i := p.cari(id)
	if i < 0 {
		return errTidakDitemukan
	}
	if p.data[i].Status != StatusDicuci {
		return errTidakBolehHapus
	}
	p.data = append(p.data[:i], p.data[i+1:]...)
	return nil
}

// ---------- Helper respons ----------

func tulisJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		status = http.StatusInternalServerError
		b = []byte(`{"galat":"gagal membentuk respons"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(b); err != nil {
		log.Printf("gagal menulis respons: %v", err)
	}
}

func tulisGalat(w http.ResponseWriter, status int, pesan string) {
	tulisJSON(w, status, map[string]string{"galat": pesan})
}

// bacaID membaca path parameter {id}; bukan angka dijawab 400.
func bacaID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		tulisGalat(w, http.StatusBadRequest, "id harus angka")
		return 0, false
	}
	return id, true
}

func galatDomain(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errTidakDitemukan):
		tulisGalat(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errDuplikat), errors.Is(err, errSudahDiambil), errors.Is(err, errTidakBolehHapus):
		tulisGalat(w, http.StatusConflict, err.Error())
	default:
		tulisGalat(w, http.StatusInternalServerError, "terjadi kesalahan pada server")
	}
}

// ---------- Handler ----------

type api struct {
	db *penyimpanan
}

func (a *api) buatCucian(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var in struct {
		Nota      string `json:"nota"`
		Pelanggan string `json:"pelanggan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		tulisGalat(w, http.StatusBadRequest, "JSON rusak atau tipe field salah")
		return
	}
	in.Nota = strings.TrimSpace(in.Nota)
	in.Pelanggan = strings.TrimSpace(in.Pelanggan)
	if in.Nota == "" || in.Pelanggan == "" {
		tulisGalat(w, http.StatusBadRequest, "field nota dan pelanggan wajib diisi")
		return
	}

	c, err := a.db.buat(in.Nota, in.Pelanggan)
	if err != nil {
		galatDomain(w, err)
		return
	}
	w.Header().Set("Location", "/cucian/"+strconv.Itoa(c.ID))
	tulisJSON(w, http.StatusCreated, c)
}

func (a *api) daftarCucian(w http.ResponseWriter, r *http.Request) {
	status := ""
	if r.URL.Query().Has("status") {
		status = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
		if status != StatusDicuci && status != StatusDiambil {
			tulisGalat(w, http.StatusBadRequest, "status tidak dikenal")
			return
		}
	}
	data := a.db.semua(status)
	tulisJSON(w, http.StatusOK, map[string]any{"jumlah": len(data), "data": data})
}

func (a *api) lihatCucian(w http.ResponseWriter, r *http.Request) {
	id, ok := bacaID(w, r)
	if !ok {
		return
	}
	c, err := a.db.ambilSatu(id)
	if err != nil {
		galatDomain(w, err)
		return
	}
	tulisJSON(w, http.StatusOK, c)
}

func (a *api) ambilCucian(w http.ResponseWriter, r *http.Request) {
	id, ok := bacaID(w, r)
	if !ok {
		return
	}
	c, err := a.db.tandaiDiambil(id)
	if err != nil {
		galatDomain(w, err)
		return
	}
	tulisJSON(w, http.StatusOK, c)
}

func (a *api) hapusCucian(w http.ResponseWriter, r *http.Request) {
	id, ok := bacaID(w, r)
	if !ok {
		return
	}
	if err := a.db.hapus(id); err != nil {
		galatDomain(w, err)
		return
	}
	tulisJSON(w, http.StatusOK, map[string]string{"status": "dihapus"})
}

func cekKesehatan(w http.ResponseWriter, _ *http.Request) {
	tulisJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func tidakDitemukan(w http.ResponseWriter, _ *http.Request) {
	tulisGalat(w, http.StatusNotFound, "endpoint tidak ditemukan")
}

func metodeTidakDiizinkan(diizinkan string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Allow", diizinkan)
		tulisGalat(w, http.StatusMethodNotAllowed, "metode HTTP tidak diizinkan, gunakan: "+diizinkan)
	}
}

func (a *api) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /cucian", a.buatCucian)
	mux.HandleFunc("GET /cucian", a.daftarCucian)
	mux.HandleFunc("GET /cucian/{id}", a.lihatCucian)
	mux.HandleFunc("POST /cucian/{id}/ambil", a.ambilCucian)
	mux.HandleFunc("DELETE /cucian/{id}", a.hapusCucian)
	mux.HandleFunc("GET /healthz", cekKesehatan)

	// Cadangan agar 404/405 juga berformat JSON {"galat": "..."}.
	mux.HandleFunc("/cucian", metodeTidakDiizinkan("GET, POST"))
	mux.HandleFunc("/cucian/{id}", metodeTidakDiizinkan("GET, DELETE"))
	mux.HandleFunc("/cucian/{id}/ambil", metodeTidakDiizinkan("POST"))
	mux.HandleFunc("/healthz", metodeTidakDiizinkan("GET"))
	mux.HandleFunc("/", tidakDitemukan)

	return mux
}

func main() {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}

	a := &api{db: baruPenyimpanan()}
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           a.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("Server Laundry Kos berjalan di port %s", port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
