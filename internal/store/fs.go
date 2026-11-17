package store

import "io/fs"

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		// The embed pattern guarantees the directory exists.
		panic(err)
	}
	return sub
}
