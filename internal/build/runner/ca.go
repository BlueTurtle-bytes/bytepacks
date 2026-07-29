package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func readCACerts(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		fmt.Printf("  → CA cert: %s (%d bytes)\n", path, len(data))
		return data, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var buf []byte
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".pem") && !strings.HasSuffix(name, ".crt") {
			continue
		}
		full := filepath.Join(path, name)
		data, err := os.ReadFile(full)
		if err != nil {
			fmt.Printf("  → CA cert: %s (skipped: %v)\n", full, err)
			continue
		}
		fmt.Printf("  → CA cert: %s (%d bytes)\n", full, len(data))
		buf = append(buf, data...)
		buf = append(buf, '\n')
	}
	if len(buf) == 0 {
		return nil, fmt.Errorf("no .pem or .crt files found in %s", path)
	}
	return buf, nil
}

func mergeCABundles(extraCAPath string) (string, error) {
	systemBundles := []string{
		"/etc/ssl/certs/ca-certificates.crt",
		"/etc/pki/tls/certs/ca-bundle.crt",
		"/etc/ssl/ca-bundle.pem",
		"/etc/ssl/cert.pem",
	}

	var systemBundle string
	var systemCerts []byte
	for _, p := range systemBundles {
		if data, err := os.ReadFile(p); err == nil {
			systemBundle = p
			systemCerts = data
			break
		}
	}
	if systemBundle != "" {
		fmt.Printf("  → system CA bundle: %s (%d bytes)\n", systemBundle, len(systemCerts))
	} else {
		fmt.Println("  → system CA bundle: not found — merged bundle will contain extra CA only")
	}

	extraCerts, err := readCACerts(extraCAPath)
	if err != nil {
		return "", fmt.Errorf("reading extra CA: %w", err)
	}

	f, err := os.CreateTemp("", "apexpack-ca-*.pem")
	if err != nil {
		return "", fmt.Errorf("creating temp CA bundle: %w", err)
	}
	defer f.Close()

	if len(systemCerts) > 0 {
		f.Write(systemCerts)
		f.Write([]byte("\n"))
	}
	f.Write(extraCerts)

	fmt.Printf("  → merged CA bundle: %s (%d bytes)\n", f.Name(), len(systemCerts)+len(extraCerts))
	return f.Name(), nil
}
