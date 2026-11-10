// Herramientas-Go: diez comandos sin dependencias externas.
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const usage = `Herramientas-Go · diez herramientas locales
Uso: herramientas COMANDO ARGUMENTOS

  hash ARCHIVO                 SHA-256 en streaming
  duplicados CARPETA           Duplicados por contenido; no borra
  backup CARPETA NUEVO.zip      ZIP fuera del origen; sin enlaces
  manifiesto CARPETA NUEVO.json Rutas, tamaños y huellas SHA-256
  verificar CARPETA JSON       Verifica un manifiesto
  grandes CARPETA CANTIDAD     Archivos de mayor tamaño
  tcp SERVIDOR PUERTO          Conexión TCP (timeout 5 segundos)
  http URL                    Cabeceras HTTP/HTTPS con HEAD
  servir CARPETA PUERTO        Servidor solo en 127.0.0.1
  csv-json ORIGEN.csv NUEVO.json CSV con cabecera a JSON

Los destinos deben ser nuevos. Las carpetas padre deben existir.
--help muestra esta ayuda. Ctrl+C detiene servir.
`

type Entry struct {
	Path string `json:"ruta"`
	Size int64  `json:"bytes"`
	Hash string `json:"sha256"`
}
type Manifest struct {
	Version int     `json:"version"`
	Files   []Entry `json:"archivos"`
}

func checksum(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func walk(root string, action func(string, fs.FileInfo) error) error {
	info, e := os.Lstat(root)
	if e != nil {
		return e
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("el origen debe ser una carpeta sin enlace")
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return action(path, info)
	})
}
func inside(root, path string) bool {
	rel, e := filepath.Rel(root, path)
	return e == nil && (rel == "." || filepath.IsLocal(rel))
}
func newFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
}
func saveJSON(path string, value any) (err error) {
	f, e := newFile(path)
	if e != nil {
		return e
	}
	defer func() {
		ce := f.Close()
		if err == nil {
			err = ce
		}
		if err != nil {
			os.Remove(path)
		}
	}()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
func jsonOut(out io.Writer, value any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
func entries(root string) ([]Entry, error) {
	result := []Entry{}
	e := walk(root, func(path string, info fs.FileInfo) error {
		h, e := checksum(path)
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		result = append(result, Entry{filepath.ToSlash(rel), info.Size(), h})
		return nil
	})
	return result, e
}
func archive(source, dest string) (err error) {
	root, e := filepath.Abs(source)
	if e != nil {
		return e
	}
	target, e := filepath.Abs(dest)
	if e != nil {
		return e
	}
	if inside(root, target) || !strings.EqualFold(filepath.Ext(target), ".zip") {
		return errors.New("usa un ZIP nuevo fuera del origen")
	}
	// Valida el origen antes de crear una salida.
	info, e := os.Lstat(root)
	if e != nil {
		return e
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("origen inválido")
	}
	f, e := newFile(target)
	if e != nil {
		return e
	}
	z := zip.NewWriter(f)
	defer func() {
		ze := z.Close()
		fe := f.Close()
		if err == nil {
			err = ze
		}
		if err == nil {
			err = fe
		}
		if err != nil {
			os.Remove(target)
		}
	}()
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if path == root || d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return nil
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		header, e := zip.FileInfoHeader(info)
		if e != nil {
			return e
		}
		header.Name = filepath.ToSlash(rel)
		if info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}
		output, e := z.CreateHeader(header)
		if e != nil {
			return e
		}
		if info.IsDir() {
			return nil
		}
		input, e := os.Open(path)
		if e != nil {
			return e
		}
		_, copyErr := io.Copy(output, input)
		closeErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}
func verify(root, manifest string) error {
	base, e := filepath.EvalSymlinks(root)
	if e != nil {
		return e
	}
	base, e = filepath.Abs(base)
	if e != nil {
		return e
	}
	f, e := os.Open(manifest)
	if e != nil {
		return e
	}
	defer f.Close()
	var data Manifest
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&data); e != nil {
		return e
	}
	if data.Version != 1 || data.Files == nil {
		return errors.New("manifiesto inválido")
	}
	for _, entry := range data.Files {
		local := filepath.FromSlash(entry.Path)
		if !filepath.IsLocal(local) {
			return errors.New("ruta fuera del origen")
		}
		path, e := filepath.EvalSymlinks(filepath.Join(base, local))
		if e != nil {
			return e
		}
		if !inside(base, path) {
			return errors.New("enlace fuera del origen")
		}
		info, e := os.Stat(path)
		if e != nil {
			return e
		}
		hash, e := checksum(path)
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() || info.Size() != entry.Size || hash != entry.Hash {
			return fmt.Errorf("archivo distinto: %s", entry.Path)
		}
	}
	return nil
}
func csvJSON(source, target string) error {
	f, e := os.Open(source)
	if e != nil {
		return e
	}
	defer f.Close()
	reader := csv.NewReader(f)
	headers, e := reader.Read()
	if e != nil {
		return e
	}
	headers[0] = strings.TrimPrefix(headers[0], "\ufeff")
	seen := map[string]bool{}
	for _, h := range headers {
		if strings.TrimSpace(h) == "" || seen[h] {
			return errors.New("cabecera vacía o repetida")
		}
		seen[h] = true
	}
	rows := []map[string]string{}
	for {
		cells, e := reader.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		row := map[string]string{}
		for i, h := range headers {
			row[h] = cells[i]
		}
		rows = append(rows, row)
	}
	return saveJSON(target, rows)
}
func port(text string) (int, error) {
	n, e := strconv.Atoi(text)
	if e != nil || n < 1 || n > 65535 {
		return 0, errors.New("puerto entre 1 y 65535")
	}
	return n, nil
}

// Cada apertura se valida, incluida la búsqueda automática de index.html.
type confinedFS struct{ root string }

func (f confinedFS) Open(name string) (http.File, error) {
	candidate := filepath.Join(f.root, filepath.FromSlash(strings.TrimPrefix(name, "/")))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil || !inside(f.root, resolved) {
		return nil, os.ErrNotExist
	}
	return os.Open(resolved)
}
func localHandler(root string) (http.Handler, error) {
	root, e := filepath.EvalSymlinks(root)
	if e != nil {
		return nil, e
	}
	root, e = filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	info, e := os.Stat(root)
	if e != nil {
		return nil, e
	}
	if !info.IsDir() {
		return nil, errors.New("se necesita una carpeta")
	}
	return http.FileServer(confinedFS{root}), nil
}
func run(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		_, e := io.WriteString(out, usage)
		return e
	}
	name := args[0]
	values := args[1:]
	required := map[string]int{"hash": 1, "duplicados": 1, "backup": 2, "manifiesto": 2, "verificar": 2, "grandes": 2, "tcp": 2, "http": 1, "servir": 2, "csv-json": 2}
	n, ok := required[name]
	if !ok || len(values) != n {
		return errors.New("comando o argumentos incorrectos; usa --help")
	}
	switch name {
	case "hash":
		h, e := checksum(values[0])
		if e != nil {
			return e
		}
		return jsonOut(out, map[string]string{"archivo": values[0], "sha256": h})
	case "duplicados":
		records, e := entries(values[0])
		if e != nil {
			return e
		}
		groups := map[string][]string{}
		for _, entry := range records {
			groups[entry.Hash] = append(groups[entry.Hash], entry.Path)
		}
		type Group struct {
			Hash  string   `json:"sha256"`
			Paths []string `json:"archivos"`
		}
		result := []Group{}
		for h, paths := range groups {
			if len(paths) > 1 {
				result = append(result, Group{h, paths})
			}
		}
		sort.Slice(result, func(i, j int) bool { return result[i].Hash < result[j].Hash })
		return jsonOut(out, result)
	case "backup":
		if e := archive(values[0], values[1]); e != nil {
			return e
		}
		return jsonOut(out, map[string]string{"creado": values[1]})
	case "manifiesto":
		root, e := filepath.Abs(values[0])
		if e != nil {
			return e
		}
		target, e := filepath.Abs(values[1])
		if e != nil {
			return e
		}
		if inside(root, target) {
			return errors.New("guarda el manifiesto fuera del origen")
		}
		records, e := entries(root)
		if e != nil {
			return e
		}
		if e = saveJSON(target, Manifest{1, records}); e != nil {
			return e
		}
		return jsonOut(out, map[string]int{"archivos": len(records)})
	case "verificar":
		if e := verify(values[0], values[1]); e != nil {
			return e
		}
		return jsonOut(out, map[string]bool{"verificado": true})
	case "grandes":
		count, e := strconv.Atoi(values[1])
		if e != nil || count < 1 || count > 10000 {
			return errors.New("cantidad entre 1 y 10000")
		}
		type Size struct {
			Path  string `json:"ruta"`
			Bytes int64  `json:"bytes"`
		}
		items := []Size{}
		e = walk(values[0], func(p string, i fs.FileInfo) error { items = append(items, Size{p, i.Size()}); return nil })
		if e != nil {
			return e
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].Bytes == items[j].Bytes {
				return items[i].Path < items[j].Path
			}
			return items[i].Bytes > items[j].Bytes
		})
		if len(items) > count {
			items = items[:count]
		}
		return jsonOut(out, items)
	case "tcp":
		p, e := port(values[1])
		if e != nil {
			return e
		}
		start := time.Now()
		conn, e := net.DialTimeout("tcp", net.JoinHostPort(values[0], strconv.Itoa(p)), 5*time.Second)
		if e != nil {
			return e
		}
		conn.Close()
		return jsonOut(out, map[string]any{"conectado": true, "milisegundos": time.Since(start).Milliseconds()})
	case "http":
		parsed, e := url.Parse(values[0])
		if e != nil {
			return e
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return errors.New("solo HTTP/HTTPS")
		}
		client := http.Client{Timeout: 10 * time.Second}
		req, e := http.NewRequest(http.MethodHead, parsed.String(), nil)
		if e != nil {
			return e
		}
		response, e := client.Do(req)
		if e != nil {
			return e
		}
		defer response.Body.Close()
		return jsonOut(out, map[string]any{"estado": response.StatusCode, "cabeceras": response.Header, "url_final": response.Request.URL.String()})
	case "servir":
		p, e := port(values[1])
		if e != nil {
			return e
		}
		handler, e := localHandler(values[0])
		if e != nil {
			return e
		}
		address := net.JoinHostPort("127.0.0.1", strconv.Itoa(p))
		fmt.Fprintln(out, "http://"+address+" · Ctrl+C para detener")
		server := http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
		return server.ListenAndServe()
	case "csv-json":
		return csvJSON(values[0], values[1])
	}
	return nil
}
func main() {
	if e := run(os.Args[1:], os.Stdout); e != nil {
		fmt.Fprintln(os.Stderr, "Error:", e)
		os.Exit(1)
	}
}
