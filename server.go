package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func startHTTPS(port int, h *hub) error {
	cert, err := loadOrCreateCert()
	if err != nil {
		return fmt.Errorf("certificate: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(indexHTML)
	})
	mux.HandleFunc("/frame", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := h.publishJPEG(data); err != nil {
			http.Error(w, "bad jpeg", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           mux,
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert}},
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0), // self-signed cert => noisy handshake errors
	}
	go func() {
		if err := srv.ServeTLS(ln, "", ""); err != nil {
			log.Printf("https: %v", err)
		}
	}()
	return nil
}

func dataDir() string {
	dir := filepath.Join(os.Getenv("APPDATA"), "PhoneCam")
	os.MkdirAll(dir, 0o700)
	return dir
}

func loadOrCreateCert() (tls.Certificate, error) {
	certPath := filepath.Join(dataDir(), "cert.pem")
	keyPath := filepath.Join(dataDir(), "key.pem")
	if c, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		if leaf, err := x509.ParseCertificate(c.Certificate[0]); err == nil && time.Until(leaf.NotAfter) > 30*24*time.Hour {
			return c, nil
		}
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	host, _ := os.Hostname()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "PhoneCam " + host, Organization: []string{"PhoneCam"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(800 * 24 * time.Hour), // Apple rejects > 825 days
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost", host},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	for _, ip := range localIPs() {
		tmpl.IPAddresses = append(tmpl.IPAddresses, net.ParseIP(ip))
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	os.WriteFile(certPath, certPEM, 0o600)
	os.WriteFile(keyPath, keyPEM, 0o600)
	return tls.X509KeyPair(certPEM, keyPEM)
}

// localIPs returns LAN IPv4 addresses, physical-looking adapters first.
func localIPs() []string {
	type cand struct {
		ip    string
		score int
	}
	var out []cand
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(ifc.Name)
		score := 0
		for _, v := range []string{"vethernet", "virtual", "vmware", "vbox", "hyper-v", "wsl", "docker", "tailscale", "zerotier", "loopback", "bluetooth"} {
			if strings.Contains(name, v) {
				score -= 10
			}
		}
		if strings.Contains(name, "wi-fi") || strings.Contains(name, "wlan") || strings.Contains(name, "ethernet") {
			score += 5
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipn.IP.To4()
			if ip4 == nil || ip4.IsLinkLocalUnicast() {
				continue
			}
			s := score
			if ip4.IsPrivate() {
				s += 3
			}
			out = append(out, cand{ip4.String(), s})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	ips := make([]string, 0, len(out))
	for _, c := range out {
		ips = append(ips, c.ip)
	}
	return ips
}
