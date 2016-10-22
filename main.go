// gitdav lets you explore a git repository via WebDAV
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"time"

	"golang.org/x/net/webdav"

	"github.com/davecheney/gitdav/internal/git"
)

const (
	defaultAddr = ":6060" // default webserver address
)

func main() {
	httpAddr := flag.String("http", defaultAddr, "HTTP service address (e.g., '"+defaultAddr+"')")
	c := flag.String("c", "", "commit to serve")

	flag.Parse()
	if len(flag.Args()) != 1 || *c == "" {
		flag.Usage()
		os.Exit(2)
	}
	repo, err := git.Open(flag.Args()[0])
	if err != nil {
		log.Fatal(err)
	}

	commit, err := repo.Commit(*c)
	if err != nil {
		log.Fatalf("%+v", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		log.Fatalf("%+v", err)
	}

	dav := webdav.Handler{
		FileSystem: &dir{root: tree},
		LockSystem: webdav.NewMemLS(),
		Logger: func(req *http.Request, err error) {
			if err != nil {
				log.Println(err)
				return
			}
			log.Printf("%v %v %v\n", req.Method, req.URL, req.Proto)
		},
	}

	log.Println("serving requests for", repo.Root, "at commit", commit)
	log.Fatalf("%+v", http.ListenAndServe(*httpAddr, &dav))
}

type dir struct {
	root *git.Tree
}

func (d *dir) Mkdir(path string, mode os.FileMode) error { return os.ErrInvalid }

func (d *dir) OpenFile(name string, flag int, perm os.FileMode) (webdav.File, error) {
	fmt.Println("OpenFile", name)
	dir, f := path.Split(name)
	if dir != "/" || f != "" {
		return &file{
			name: f,
			tree: d.root,
		}, nil
	}
	return &file{
		name: dir,
		tree: d.root,
	}, nil
}

func (d *dir) RemoveAll(name string) error {
	return os.ErrInvalid
}

func (d *dir) Rename(oldName, newName string) error {
	return os.ErrInvalid
}

func (d *dir) Stat(name string) (os.FileInfo, error) {
	return &fileinfo{name: name, mode: os.ModeDir | 0644}, nil
}

type file struct {
	name string
	tree *git.Tree
}

func (f *file) Close() error             { return nil }
func (f *file) Read([]byte) (int, error) { return 0, os.ErrInvalid }
func (f *file) Readdir(int) ([]os.FileInfo, error) {
	// TODO(dfc) respect n
	var entries []os.FileInfo
	for _, e := range f.tree.Entries {
		entries = append(entries, &fileinfo{name: e.Name, mode: os.FileMode(e.Mode)})
	}
	return entries, nil
}

func (f *file) Seek(offset int64, whence int) (int64, error) { return 0, os.ErrInvalid }
func (f *file) Stat() (os.FileInfo, error) {
	return &fileinfo{name: f.name, mode: os.ModeDir | 0644}, nil
}
func (f *file) Write(p []byte) (int, error) { return 0, os.ErrInvalid }

type fileinfo struct {
	name string
	size int64
	mode os.FileMode
}

func (fi *fileinfo) Name() string       { return fi.name }
func (fi *fileinfo) Size() int64        { return fi.size }
func (fi *fileinfo) Mode() os.FileMode  { return fi.mode }
func (fi *fileinfo) ModTime() time.Time { return time.Now() }
func (fi *fileinfo) IsDir() bool        { return fi.Mode().IsDir() }
func (fi *fileinfo) Sys() interface{}   { return nil }
