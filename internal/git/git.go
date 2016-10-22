// git manipulates on disk git repositories.
package git

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
)

// Repository represents a git repository.
type Repository struct {

	// Root is the base path to the repository
	Root string
}

// Open returns a Repository representing the git repository
// that contains path. Open walks up the directory heirarchy
// until it finds a path with a .git, or it hits the root of
// the file system.
func Open(p string) (*Repository, error) {
	path, err := filepath.Abs(p)
	if err != nil {
		return nil, errors.Wrapf(err, "could not convert path %q to an absolute path", p)
	}

	for path != string(filepath.Separator) {
		gitdir := filepath.Join(path, ".git")
		if fi, err := os.Stat(gitdir); err != nil {
			if !os.IsNotExist(err) {
				return nil, errors.WithStack(err)
			}
		} else {
			if fi.IsDir() {
				return &Repository{
					Root: path,
				}, nil
			}
		}
		path = filepath.Dir(path)
	}
	path, _ = filepath.Abs(p) // ignore error, we checked it already
	return nil, errors.Errorf("could not locate git repository for path %q", path)
}

// Tree represents a tree object.
type Tree struct {
	*Commit

	// id is the SHA1 of this tree
	id string

	// entries are the
	Entries []Entry
}

type Blob struct {
	Size int64
	io.ReadCloser
}

// Blob is a convenience method for returning a git blob object that is a child of the current tree.
func (t *Tree) Blob(name string) (*Blob, error) {
	for _, e := range t.Entries {
		if name == e.Name {
			return t.readBlob(e.id)
		}
	}
	return nil, &os.PathError{
		Op:   "open",
		Path: name,
		Err:  os.ErrNotExist,
	}
}

// Tree is a convenience method for returning a git tree object that is a child of the current tree.
func (t *Tree) Tree(name string) (*Tree, error) {
	for _, e := range t.Entries {
		if name == e.Name {
			return t.readTree(e.id)
		}
	}
	return nil, &os.PathError{
		Op:   "open",
		Path: name,
		Err:  os.ErrNotExist,
	}
}

// readBlob returns a git blob object.
func (t *Tree) readBlob(sha string) (*Blob, error) {
	h, rc, err := t.readObject(sha)
	if err != nil {
		return nil, err
	}
	if h.kind != "blob" {
		return nil, errors.Errorf("expected blob, got %q", h.kind)
	}
	return &Blob{
		Size:       h.length,
		ReadCloser: rc,
	}, nil
}

type Entry struct {
	*Tree // parent tree of this entry

	Name string
	Mode os.FileMode
	id   string
}

func scanTreeEntry(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	recordLength := bytes.IndexByte(data, '\x00') + 21
	if recordLength <= len(data) {
		return recordLength, data[:recordLength], nil
	}

	if atEOF {
		return 0, nil, errors.Errorf("malformed record %q", data)
	}
	return 0, nil, nil
}

// parseTree parses a tree object from the supplied io.Reader.
func (t *Tree) parseTree(r io.Reader) (*Tree, error) {
	sc := bufio.NewScanner(r)
	sc.Split(scanTreeEntry)
	for sc.Scan() {
		buf := sc.Bytes()
		buf, sha := buf[:len(buf)-21], buf[len(buf)-20:]
		var name string
		var mode os.FileMode
		if _, err := fmt.Fscanf(bytes.NewReader(buf), "%d %s", &mode, &name); err != nil {
			return nil, errors.Wrap(err, "could not read tree entry")
		}

		// TODO(dfc)
		// if the blob is _not_ present on disk (ie, it's in a pack file)
		// then do not return it in the entries set.
		// Obviously we need to implement pack support, but yolo
		path := filepath.Join(t.Root, ".git", "objects", string(sha)[0:2], string(sha)[2:])
		if _, err := os.Stat(path); os.IsNotExist(err) {
			//	continue
		}

		t.Entries = append(t.Entries, Entry{
			Tree: t,
			Name: name,
			Mode: mode,
			id:   fmt.Sprintf("%x", string(sha)),
		})
	}
	return t, sc.Err()
}

// Commit represents a commit object.
type Commit struct {
	*Repository

	// id of the tree object.
	tree string

	// id is the SHA1 of this commit
	id string
}

func (c *Commit) String() string { return c.id }

// Tree returns the Tree object for this commit.
func (c *Commit) Tree() (*Tree, error) {
	return c.readTree(c.tree)
}

// Commit returns a Commit matching the supplied id.
func (r *Repository) Commit(sha string) (*Commit, error) {
	return r.readCommit(sha)
}

// readCommit reads a commit object.
func (r *Repository) readCommit(sha string) (*Commit, error) {
	h, rc, err := r.readObject(sha)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	if h.kind != "commit" {
		return nil, errors.Errorf("expected commit, got %q", h.kind)
	}
	c := Commit{
		Repository: r,
		id:         sha,
	}

	return c.parseCommit(rc)
}

// parseCommit parses a commit object from the supplied io.Reader.
func (c *Commit) parseCommit(r io.Reader) (*Commit, error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		s := sc.Text()
		i := strings.Index(s, " ")
		if i < 0 {
			// ignore this line
			continue
		}
		switch s[:i] {
		case "tree":
			c.tree = strings.TrimSpace(s[len("tree "):])
		}
	}
	return c, sc.Err()
}

// header is a git header.
type header struct {
	kind   string
	length int64
}

// readObject returns a header and an io.ReadCloser for a git object.
func (r *Repository) readObject(sha string) (header, io.ReadCloser, error) {
	path := filepath.Join(r.Root, ".git", "objects", sha[0:2], sha[2:])
	f, err := os.Open(path)
	if err != nil {
		return header{}, nil, errors.WithStack(err)
	}
	fr, err := zlib.NewReader(f)
	if err != nil {
		return header{}, nil, errors.WithStack(err)
	}

	var kind string
	var length int64
	if _, err := fmt.Fscanf(fr, "%s %d\u0000", &kind, &length); err != nil {
		return header{}, nil, errors.Wrap(err, "cannot parse header")
	}

	return header{
			kind:   kind,
			length: length,
		}, struct {
			io.Reader
			io.Closer
		}{
			fr, // TODO(use a limit reader to clamp body size to length)
			f,
		}, nil
}

// readTree reads a tree object.
func (c *Commit) readTree(sha string) (*Tree, error) {
	h, rc, err := c.readObject(sha)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	if h.kind != "tree" {
		return nil, errors.Errorf("expected tree, got %q", h.kind)
	}
	t := Tree{
		Commit: c,
		id:     sha,
	}

	return t.parseTree(rc)
}
