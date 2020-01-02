package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/billziss-gh/cgofuse/fuse"
	"github.com/gomodule/redigo/redis"
)

/*
	redis-cli -n 15 -x SET s3rj1k.xyz.crt <certs/s3rj1k.xyz.crt
	redis-cli -n 15 -x SET s3rj1k.xyz.key <certs/s3rj1k.xyz.key

	redis-cli -n 15 -x SET .s3rj1k.xyz.crt <certs/.s3rj1k.xyz.crt
	redis-cli -n 15 -x SET .s3rj1k.xyz.key <certs/.s3rj1k.xyz.key

	redis-cli -n 15 -x SET test1.domain.com.crt <certs/test1.domain.com.crt
	redis-cli -n 15 -x SET test1.domain.com.key <certs/test1.domain.com.key

	redis-cli -n 15 -x SET .wilddomain.com.crt <certs/.wilddomain.com.crt
	redis-cli -n 15 -x SET .wilddomain.com.key <certs/.wilddomain.com.key

	redis-cli -n 15 -x SET test.s3rj1k.xyz.crt <certs/test.s3rj1k.xyz.crt
	redis-cli -n 15 -x SET test.s3rj1k.xyz.key <certs/test.s3rj1k.xyz.key

	redis-cli -n 15 -x SET .test.s3rj1k.xyz.crt <certs/.test.s3rj1k.xyz.crt
	redis-cli -n 15 -x SET .test.s3rj1k.xyz.key <certs/.test.s3rj1k.xyz.key

	openssl x509 -text -noout -in {DOMAIN}
*/

const (
	dbAddress     = "127.0.0.1:6379"
	pathSeparator = "/"
)

// CertFS describes fuse based certificate filesystem backed by Redis.
type CertFS struct {
	fuse.FileSystemBase

	db             *redis.Pool
	reDomainPrefix *regexp.Regexp
}

// Init initializes fuse based filesystem.
func (fs *CertFS) Init() {
	fs.db = &redis.Pool{
		MaxIdle:   2,
		MaxActive: 20,
		Dial: func() (redis.Conn, error) {
			return redis.Dial(
				"tcp",
				dbAddress,
				redis.DialDatabase(15),
			)
		},
	}

	fs.reDomainPrefix = regexp.MustCompile(`^.*?\.`)
}

// PathToKeys returns keys for K/V DB computed from provided paths.
func (fs *CertFS) PathToKeys(path string) []string {
	exact := strings.ToLower(
		strings.TrimPrefix(
			filepath.Clean(path), pathSeparator,
		),
	)
	wildcard := fs.reDomainPrefix.ReplaceAllString(exact, ".")

	return []string{exact, wildcard}
}

// Open implements 'Open' syscall.
func (fs *CertFS) Open(path string, _ /*flags*/ int) (errc int, fh uint64) {
	c := fs.db.Get()
	defer c.Close()

	for _, key := range fs.PathToKeys(path) {
		if n, _ := redis.Int(c.Do("EXISTS", key)); n > 0 {
			return 0, 0
		}
	}

	return -fuse.ENOENT, ^uint64(0)
}

// Getattr implements 'Getattr' syscall.
func (fs *CertFS) Getattr(path string, stat *fuse.Stat_t, _ /*fh*/ uint64) (errc int) {
	if strings.HasSuffix(path, pathSeparator) {
		stat.Mode = fuse.S_IFDIR | 0555

		return 0
	}

	c := fs.db.Get()
	defer c.Close()

	for _, key := range fs.PathToKeys(path) {
		if n, _ := redis.Int(c.Do("EXISTS", key)); n > 0 {
			stat.Mode = fuse.S_IFREG | 0444

			return 0
		}
	}

	return -fuse.ENOENT
}

// Read implements 'Read' syscall.
func (fs *CertFS) Read(path string, buff []byte, ofset int64, _ /*fh*/ uint64) int {
	c := fs.db.Get()
	defer c.Close()

	for _, key := range fs.PathToKeys(path) {
		data, err := redis.Bytes(
			c.Do("GET", key),
		)
		if err != nil || len(data) == 0 {
			continue
		}

		endOfset := ofset + int64(len(buff))
		dataLen := int64(len(data))

		if endOfset > dataLen {
			endOfset = dataLen
		}

		if endOfset < ofset {
			return 0
		}

		n := copy(buff, data[ofset:endOfset])

		return n
	}

	return 0
}

// Readdir implements 'Readdir' syscall.
func (fs *CertFS) Readdir(_ /*path*/ string, fill func(name string, stat *fuse.Stat_t, ofset int64) bool, _ /*ofset*/ int64, _ /*fh*/ uint64) (errc int) {
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
