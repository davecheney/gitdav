// gitdav lets you explore a git repository via WebDAV
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"golang.org/x/net/webdav"
)

const (
	defaultAddr = ":6060" // default webserver address
)

func main() {
	httpAddr := flag.String("http", defaultAddr, "HTTP service address (e.g., '"+defaultAddr+"')")

	flag.Parse()
	if len(flag.Args()) != 1 {
		flag.Usage()
		os.Exit(2)
	}
	root, err := filepath.Abs(flag.Args()[0])
	if err != nil {
		log.Fatal(err)
	}

	dav := webdav.Handler{
		FileSystem: webdav.Dir(root),
		LockSystem: webdav.NewMemLS(),
		Logger: func(req *http.Request, err error) {
			if err != nil {
				log.Println(err)
				return
			}
			log.Printf("%v %v %v\n", req.Method, req.URL, req.Proto)
		},
	}

	log.Fatal(http.ListenAndServe(*httpAddr, &dav))
}
