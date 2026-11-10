package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestHashAndDuplicates(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a"), []byte("hello"), 0600)
	os.WriteFile(filepath.Join(dir, "b"), []byte("hello"), 0600)
	h, e := checksum(filepath.Join(dir, "a"))
	if e != nil || h != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatal(h, e)
	}
	var out bytes.Buffer
	if e := run([]string{"duplicados", dir}, &out); e != nil {
		t.Fatal(e)
	}
	var rows []map[string]any
	json.Unmarshal(out.Bytes(), &rows)
	if len(rows) != 1 {
		t.Fatal(out.String())
	}
}
func TestBackupAndExistingDestination(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	os.Mkdir(source, 0700)
	os.Mkdir(filepath.Join(source, "empty"), 0700)
	os.WriteFile(filepath.Join(source, "data.txt"), []byte("datos"), 0600)
	target := filepath.Join(root, "copy.zip")
	if e := archive(source, target); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(target)
	if e := archive(source, target); e == nil {
		t.Fatal("sobrescritura")
	}
	after, _ := os.ReadFile(target)
	if !bytes.Equal(before, after) {
		t.Fatal("se modificó el destino")
	}
	z, e := zip.OpenReader(target)
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	if len(z.File) != 2 {
		t.Fatal("faltan datos o carpeta vacía")
	}
	if e := archive(source, filepath.Join(source, "bad.zip")); e == nil {
		t.Fatal("destino dentro del origen")
	}
}
func TestManifestTamperingAndEscape(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	os.Mkdir(source, 0700)
	os.WriteFile(filepath.Join(source, "a"), []byte("original"), 0600)
	items, _ := entries(source)
	path := filepath.Join(root, "manifest.json")
	if e := saveJSON(path, Manifest{1, items}); e != nil {
		t.Fatal(e)
	}
	if e := verify(source, path); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(source, "a"), []byte("cambio"), 0600)
	if e := verify(source, path); e == nil {
		t.Fatal("no detecta cambios")
	}
	bad := filepath.Join(root, "bad.json")
	saveJSON(bad, Manifest{1, []Entry{{"../outside", 0, ""}}})
	if e := verify(source, bad); e == nil {
		t.Fatal("no bloquea escape")
	}
}
func TestCSVAndErrors(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "data.csv")
	dst := filepath.Join(root, "data.json")
	os.WriteFile(src, []byte("nombre,nota\nAna,\"uno\ndos\"\n"), 0600)
	if e := csvJSON(src, dst); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(dst)
	var rows []map[string]string
	if e := json.Unmarshal(data, &rows); e != nil || rows[0]["nota"] != "uno\ndos" {
		t.Fatal(string(data), e)
	}
	if e := csvJSON(src, dst); e == nil {
		t.Fatal("sobrescritura")
	}
	if e := run([]string{"tcp", "localhost", "0"}, &bytes.Buffer{}); e == nil {
		t.Fatal("puerto inválido")
	}
}
func TestLocalServerAndHttp(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "index.html"), []byte("local"), 0600)
	handler, e := localHandler(root)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	var out bytes.Buffer
	if e := run([]string{"http", server.URL}, &out); e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(out.Bytes(), []byte("200")) {
		t.Fatal(out.String())
	}
}
func TestSymlinkOutsideIsNotServed(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0600)
	if e := os.Symlink(outside, filepath.Join(root, "link")); e != nil {
		t.Skip("el equipo no permite enlaces")
	}
	handler, e := localHandler(root)
	if e != nil {
		t.Fatal(e)
	}
	request := httptest.NewRequest("GET", "/link/secret.txt", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 404 {
		t.Fatal(response.Code)
	}
}

func TestIndexSymlinkOutsideIsNotServed(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("CONTENIDO_PRIVADO"), 0600)
	if e := os.Symlink(outside, filepath.Join(root, "index.html")); e != nil {
		t.Skip("el equipo no permite enlaces")
	}
	handler, e := localHandler(root)
	if e != nil {
		t.Fatal(e)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	if bytes.Contains(response.Body.Bytes(), []byte("CONTENIDO_PRIVADO")) {
		t.Fatal("se filtró index.html por un enlace")
	}
}

func TestTCPOnLocalListener(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	p := listener.Addr().(*net.TCPAddr).Port
	var out bytes.Buffer
	if e := run([]string{"tcp", "127.0.0.1", strconv.Itoa(p)}, &out); e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(out.Bytes(), []byte("true")) {
		t.Fatal(out.String())
	}
}
func TestLargestOrder(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "small"), []byte("a"), 0600)
	os.WriteFile(filepath.Join(root, "large"), []byte("abcdef"), 0600)
	var out bytes.Buffer
	if e := run([]string{"grandes", root, "1"}, &out); e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(out.Bytes(), []byte("large")) || bytes.Contains(out.Bytes(), []byte("small")) {
		t.Fatal(out.String())
	}
}
