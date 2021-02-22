package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"syscall"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandomStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

var privateIPBlocks []*net.IPNet

func init() {
	for _, cidr := range []string{
		"10.0.0.0/8",     // RFC1918
		"172.16.0.0/12",  // RFC1918
		"192.168.0.0/16", // RFC1918
		"169.254.0.0/16", // RFC3927 link-local
	} {
		_, block, err := net.ParseCIDR(cidr)
		if err != nil {
			log.Panic(fmt.Errorf("parse error on %q: %v", cidr, err))
		}
		privateIPBlocks = append(privateIPBlocks, block)
	}
}

func isPrivateIP(ip net.IP) bool {
	for _, block := range privateIPBlocks {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

func privateIP() string {
	net.InterfaceAddrs()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Printf("Error: %v", err)
		return "0.0.0.0"
	}

	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if ok && isPrivateIP(ipnet.IP) {
			return ipnet.IP.String()
		}
	}
	return "0.0.0.0"
}

const host = "0.0.0.0"
const port = "8100"

type Server struct {
	dir  string
	host string
	port string
	ttl  time.Duration
}

func NewTempServer() *Server {
	dir, err := ioutil.TempDir(os.TempDir(), "")
	if err != nil {
		log.Fatal(err)
	}
	return &Server{
		dir:  dir,
		host: host,
		port: port,
		ttl:  0,
	}
}

func (s *Server) RemoveAll() error {
	switch s.dir {
	case "", "/":
		return nil
	}
	return os.RemoveAll(s.dir)
}

func (s *Server) Start() error {
	http.Handle("/files/", http.StripPrefix("/files/", http.FileServer(http.Dir(s.dir))))
	http.HandleFunc("/dir", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(s.dir))
	})

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		s.RemoveAll()
	}()

	err := http.ListenAndServe(":"+s.port, nil)
	if err != nil {
		s.RemoveAll()
		return err
	}
	return nil
}

type Client struct {
	host string
	port string
}

func (c *Client) Dir() (string, error) {
	resp, err := http.Get(fmt.Sprintf("http://%s:%s/dir", c.host, c.port))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (c *Client) Put(name string) error {
	dir, err := c.Dir()
	if err != nil {
		return err
	}
	randDir := path.Join(dir, RandomStringRunes(40))
	err = os.MkdirAll(randDir, 0755)
	if err != nil {
		return err
	}

	base := filepath.Base(name)
	dst, err := os.Create(filepath.Join(randDir, base))
	if err != nil {
		return err
	}
	defer dst.Close()

	src, err := os.OpenFile(name, os.O_RDONLY, 0x0)
	if err != nil {
		return err
	}
	defer src.Close()

	body, err := ioutil.ReadAll(src)
	if err != nil {
		return err
	}
	_, err = dst.Write(body)
	if err != nil {
		return err
	}
	fmt.Printf("http://%s:%s/files/%s/%s\n", privateIP(), port, path.Base(randDir), base)
	return nil
}

func main() {
	s := flag.Bool("server", false, "")
	flag.Parse()

	if *s {
		srv := NewTempServer()
		log.Printf("Serving on http://%s:%s\n", privateIP(), srv.port)
		log.Fatal(srv.Start())
	} else {
		client := Client{
			host: host,
			port: port,
		}

		arg := flag.Arg(0)
		if arg != "" {
			err := client.Put(arg)
			if err != nil {
				log.Fatal(err)
			}
		}
	}
}
