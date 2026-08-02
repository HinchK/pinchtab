package profiles

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func copyDir(src *importSource, dst string) error {
	return fs.WalkDir(src.root.FS(), filepath.ToSlash(src.relative), func(sourcePath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not allowed in imported profiles: %s", sourcePath)
		}

		rel, err := filepath.Rel(filepath.FromSlash(src.relative), filepath.FromSlash(sourcePath))
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))

		if d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(target, info.Mode().Perm())
		}

		return copyFile(src.root, filepath.FromSlash(sourcePath), target)
	})
}

func copyFile(root *os.Root, src, dst string) error {
	in, err := root.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s: %w", src, err)
	}
	return out.Close()
}
