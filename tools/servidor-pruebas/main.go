// servidor-pruebas sirve una carpeta por HTTP en 127.0.0.1 con soporte de Range,
// para ensayar una publicación sin subir nada a GitHub:
//
//	go run ./tools/servidor-pruebas --dir dist/release --port 8765
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
)

func main() {
	dir := flag.String("dir", ".", "carpeta a servir")
	port := flag.Int("port", 8765, "puerto")
	flag.Parse()
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	log.Println("Sirviendo", *dir, "en", addr)
	log.Fatal(http.ListenAndServe(addr, http.FileServer(http.Dir(*dir))))
}
