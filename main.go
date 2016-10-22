// gitdav lets you explore a git repository via WebDAV
package main

import (
	"flag"
	"io"
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
				log.Printf("%+v", err)
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
	dir, f := path.Split(name)
	if dir == "/" && f == "" {
		return &tree{
			name: dir,
			tree: d.root,
		}, nil
	}

	if dir == "/" {
		// local file
		b, err := d.root.Blob(f)
		if err == nil {
			return &blob{
				name: f,
				Blob: b,
			}, nil
		}

		t, err := d.root.Tree(f)
		if err != nil {
			return nil, err
		}
		return &tree{
			name: f,
			tree: t,
		}, nil
	}

	t, err := d.root.Tree(dir)
	if err != nil {
		return nil, err
	}
	return &tree{
		name: f,
		tree: t,
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

type tree struct {
	name string
	tree *git.Tree
}

func (t *tree) Close() error             { return nil }
func (t *tree) Read([]byte) (int, error) { return 0, os.ErrInvalid }
func (t *tree) Readdir(int) ([]os.FileInfo, error) {
	// TODO(dfc) respect n
	var entries []os.FileInfo
	for _, e := range t.tree.Entries {
		b, err := t.tree.Blob(e.Name)
		if err != nil {
			entries = append(entries, &fileinfo{name: e.Name, mode: e.Mode})
		} else {
			entries = append(entries, &fileinfo{name: e.Name, size: b.Size, mode: e.Mode})
		}
	}
	return entries, nil
}

func (t *tree) Seek(offset int64, whence int) (int64, error) {
	return 0, os.ErrInvalid
}
func (t *tree) Stat() (os.FileInfo, error) {
	return &fileinfo{name: t.name, mode: os.ModeDir | 0644}, nil
}
func (t *tree) Write(p []byte) (int, error) { return 0, os.ErrInvalid }

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

type blob struct {
	name string
	*git.Blob
}

func (b *blob) Readdir(int) ([]os.FileInfo, error) { return nil, os.ErrInvalid }

func (b *blob) Seek(offset int64, whence int) (int64, error) {
	// work around the way net/http.ServeContent's seeking to the end then
	// rewind to the start behaviour to get the size of a file ...
	switch {
	case offset == 0 && whence == io.SeekEnd:
		return b.Size, nil
	case offset == 0 && whence == io.SeekStart:
		return 0, nil
	default:
		return 0, os.ErrInvalid
	}
}
func (b *blob) Stat() (os.FileInfo, error) {
	return &fileinfo{name: b.name, size: b.Size, mode: 0644}, nil
}
func (b *blob) Write(p []byte) (int, error) { return 0, os.ErrInvalid }
