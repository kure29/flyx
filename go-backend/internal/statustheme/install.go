package statustheme

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Limits on a theme package. The upload itself is capped by the caller at
// MaxPackage; the rest bound what the gzip stream may expand to.
const (
	MaxPackage  = 32 << 20
	maxEntries  = 2000
	maxFile     = 8 << 20
	maxExpanded = 64 << 20
	// What may follow tar's end marker: padding to a whole record.
	maxPadding = 1 << 20
	// Every byte the stream may expand to, tar headers included.
	maxStream = maxExpanded + maxPadding + (8 << 20)
)

const (
	msgDamaged = "主题包损坏或不完整（可能没下载完），重新下载 theme.tar.gz 再试"
	msgNotGzip = "选的不是主题包：到主题仓库的 Releases 下载 theme.tar.gz，不要选 Source code"
)

func randomSuffix() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Install unpacks a theme.tar.gz and puts it in place under the short its
// manifest names. Nothing is visible under that name until the package has
// been fully unpacked and checked; a theme being replaced is moved aside and
// restored if the swap fails. expect, when set, is the short the package
// must carry (an update may not install a different theme).
func (s *Store) Install(archive io.Reader, expect string) (Theme, error) {
	if s == nil || s.Dir == "" {
		return Theme{}, errors.New("themes directory not configured")
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return Theme{}, err
	}
	// A leading dot keeps work in progress out of List (ValidShort refuses it).
	staging := filepath.Join(s.Dir, ".staging-"+randomSuffix())
	t, err := unpack(archive, staging)
	if err == nil {
		t, err = s.publish(staging, t, expect)
	}
	if err != nil {
		_ = os.RemoveAll(staging)
	}
	return t, err
}

func unpack(archive io.Reader, into string) (Theme, error) {
	br := bufio.NewReader(archive)
	if magic, err := br.Peek(2); err != nil || magic[0] != 0x1f || magic[1] != 0x8b {
		return Theme{}, Error(msgNotGzip)
	}
	gz, err := gzip.NewReader(br)
	if err != nil {
		return Theme{}, Error(msgDamaged)
	}
	gz.Multistream(false)
	stream := &io.LimitedReader{R: gz, N: maxStream}

	if err := os.Mkdir(into, 0o755); err != nil {
		return Theme{}, err
	}
	root, err := os.OpenRoot(into)
	if err != nil {
		return Theme{}, err
	}
	defer root.Close()

	entriesErr := unpackEntries(tar.NewReader(stream), root)
	if entriesErr == nil {
		// Read on to the end, where gzip keeps its checksum: the tar entries
		// stop at the end marker, short of it.
		if _, err := io.Copy(io.Discard, io.LimitReader(stream, maxPadding)); err != nil {
			return Theme{}, Error(msgDamaged)
		}
		var one [1]byte
		if n, err := stream.Read(one[:]); n != 0 {
			return Theme{}, refuse("主题包在 tar 结尾之后还有多余的数据，包本身有问题，请联系主题作者")
		} else if err != nil && err != io.EOF {
			return Theme{}, Error(msgDamaged)
		}
	}
	if stream.N <= 0 {
		return Theme{}, refuse("主题包解压后超过 %d MiB", maxExpanded>>20)
	}
	if entriesErr != nil {
		return Theme{}, entriesErr
	}

	data, err := readCapped(root, manifestFile, maxManifest)
	if err != nil {
		return Theme{}, refuse("主题包里没有 theme.json（或者它过大）；下载的若是 Source code，换成 Releases 里的 theme.tar.gz")
	}
	t, err := parseManifest(data)
	if err != nil {
		return Theme{}, err
	}
	if t.Short == DefaultShort {
		return Theme{}, refuse("主题的 short 不能是 %q，它留给内置主题", DefaultShort)
	}
	if st, err := root.Stat(distDir + "/" + indexFile); err != nil || !st.Mode().IsRegular() {
		return Theme{}, refuse("主题包里没有 dist/index.html")
	}
	return t, nil
}

func unpackEntries(tr *tar.Reader, root *os.Root) error {
	var expanded int64
	dirs := map[string]bool{}
	files := 0
	for seen := 0; ; seen++ {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return Error(msgDamaged)
		}
		if seen >= maxEntries {
			return refuse("主题包里的条目超过 %d 个", maxEntries)
		}
		switch hdr.Typeflag {
		case tar.TypeXGlobalHeader:
			continue
		case tar.TypeReg, tar.TypeDir:
		default:
			// Links and devices belong in no theme, and are a way to write
			// outside the directory.
			return refuse("主题包里有不支持的条目：%s", hdr.Name)
		}
		if hdr.Size > maxFile {
			return refuse("%s 超过单个文件 %d MiB 的上限", hdr.Name, maxFile>>20)
		}
		if expanded > maxExpanded-hdr.Size {
			return refuse("主题包解压后超过 %d MiB", maxExpanded>>20)
		}
		expanded += hdr.Size

		name := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(hdr.Name, "./")), "/")
		if len(hdr.Name) > 1024 || strings.HasPrefix(hdr.Name, "/") || strings.Contains(hdr.Name, "\\") || !fs.ValidPath(name) {
			return refuse("主题包里的路径越出了主题目录：%s", hdr.Name)
		}
		// path.Clean of a rooted path already removed any "..", but a name
		// that needed it is not one a packer writes.
		for _, part := range strings.Split(strings.TrimPrefix(hdr.Name, "./"), "/") {
			if part == ".." {
				return refuse("主题包里的路径越出了主题目录：%s", hdr.Name)
			}
		}
		if name == "." {
			continue
		}

		dir := name
		if hdr.Typeflag == tar.TypeReg {
			dir = path.Dir(name)
			files++
		}
		if err := mkdirs(root, dir, dirs); err != nil {
			return err
		}
		if files+len(dirs) > maxEntries {
			return refuse("主题包里的文件和目录超过 %d 个", maxEntries)
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return refuse("主题包里有同名的文件和目录，包本身有问题：%s", hdr.Name)
		}
		_, err = io.Copy(f, io.LimitReader(tr, maxFile))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) {
				return Error(msgDamaged)
			}
			return err
		}
	}
}

// mkdirs creates dir and its parents inside root, counting each once.
func mkdirs(root *os.Root, dir string, made map[string]bool) error {
	if dir == "." || dir == "" || made[dir] {
		return nil
	}
	if err := mkdirs(root, path.Dir(dir), made); err != nil {
		return err
	}
	if err := root.Mkdir(dir, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return refuse("主题包里有同名的文件和目录，包本身有问题：%s", dir)
	}
	if st, err := root.Lstat(dir); err != nil || !st.IsDir() {
		return refuse("主题包里有同名的文件和目录，包本身有问题：%s", dir)
	}
	made[dir] = true
	return nil
}

func (s *Store) publish(staging string, t Theme, expect string) (Theme, error) {
	if expect != "" && expect != t.Short {
		return Theme{}, refuse("这个包里是主题 %q，不是 %q", t.Short, expect)
	}
	dest := filepath.Join(s.Dir, t.Short)
	aside := filepath.Join(s.Dir, ".replaced-"+randomSuffix())
	replacing := false
	if _, err := os.Lstat(dest); err == nil {
		if err := os.Rename(dest, aside); err != nil {
			return Theme{}, err
		}
		replacing = true
	}
	if err := os.Rename(staging, dest); err != nil {
		if replacing {
			_ = os.Rename(aside, dest)
		}
		return Theme{}, err
	}
	if replacing {
		_ = os.RemoveAll(aside)
	}
	t, ok := s.installed(t.Short)
	if !ok {
		return Theme{}, errors.New("installed theme cannot be read back")
	}
	return t, nil
}
