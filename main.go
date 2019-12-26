package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/billziss-gh/cgofuse/fuse"
	"github.com/gomodule/redigo/redis"
)

/*
	redis-cli -x SET s3rj1k.xyz.crt <s3rj1k.xyz.crt
	redis-cli -x SET .s3rj1k.xyz.crt <.s3rj1k.xyz.crt
	redis-cli -x SET s3rj1k.xyz.key <s3rj1k.xyz.key
	redis-cli -x SET .s3rj1k.xyz.key <.s3rj1k.xyz.key
*/

const (
	dbAddress     = "127.0.0.1:6379"
	pathSeparator = "/"
)

type CertFS struct {
	fuse.FileSystemBase

	db *redis.Pool
}

func (fs *CertFS) Init() {
	fs.db = &redis.Pool{
		MaxIdle:   2,
		MaxActive: 20,
		Dial: func() (redis.Conn, error) {
			return redis.Dial(
				"tcp",
				dbAddress,
			)
		},
	}
}

func PathToKey(path string) string {
	return strings.TrimPrefix(filepath.Clean(path), pathSeparator)
}

func (fs *CertFS) Open(path string, flags int) (errc int, fh uint64) {
	c := fs.db.Get()
	defer c.Close()

	if ok, _ := redis.Bool(
		c.Do("EXISTS", PathToKey(path)),
	); ok {
		return 0, 0
	}

	return -fuse.ENOENT, ^uint64(0)
}

func (fs *CertFS) Getattr(path string, stat *fuse.Stat_t, fh uint64) (errc int) {
	if strings.HasSuffix(path, pathSeparator) {
		stat.Mode = fuse.S_IFDIR | 0555

		return 0
	}

	c := fs.db.Get()
	defer c.Close()

	if ok, _ := redis.Bool(
		c.Do("EXISTS", PathToKey(path)),
	); ok {
		stat.Mode = fuse.S_IFREG | 0444

		return 0
	}

	return -fuse.ENOENT
}

func (fs *CertFS) Read(path string, buff []byte, ofset int64, fh uint64) (n int) {
	c := fs.db.Get()
	defer c.Close()

	data, err := redis.Bytes(
		c.Do("GET", PathToKey(path)),
	)
	if err != nil {
		return 0
	}

	endOfset := ofset + int64(len(buff))
	dataLen := int64(len(data))

	if endOfset > dataLen {
		endOfset = dataLen
	}

	if endOfset < ofset {
		return 0
	}

	n = copy(buff, data[ofset:endOfset])

	return
}

func (fs *CertFS) Readdir(_ string, fill func(name string, stat *fuse.Stat_t, ofst int64) bool, ofst int64, _ uint64) (errc int) {
	fill(".", nil, 0)
	fill("..", nil, 0)

	c := fs.db.Get()
	defer c.Close()

	paths, err := redis.Strings(
		c.Do("KEYS", "*"),
	)
	if err != nil {
		return 0
	}

	for i := range paths {
		fill(paths[i], nil, 0)
	}

	return 0
}

func main() {
	CertFS := new(CertFS)

	host := fuse.NewFileSystemHost(CertFS)

	// http://man7.org/linux/man-pages/man8/mount.fuse.8.html
	host.Mount(
		os.Args[1],
		[]string{
			"-o",
			"ro,nosuid,nodev,noexec,noatime,allow_other,auto_unmount,direct_io",
		},
	)
}
