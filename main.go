package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

type ForwardGroup struct {
	Listen      string `json:"listen"`
	ListenPort  string
	ListenHost  string
	Forward     string `json:"forward"`
	ForwardPort string
	ForwardHost string
	Proto       string `json:"proto,omitempty"`
}

func main() {
	listenPort := flag.String("l", "", "local endpoint listens on")
	forwardPort := flag.String("f", "", "remote endpoint forwards to")
	configPath := flag.String("c", "", "Config file path")
	flag.Parse()

	if *configPath != "" {
		fgs, err := readConfig(*configPath)
		if fgs == nil {
			fmt.Printf("open file error or wrong config file format: %v\n", err)
			os.Exit(1)
		}

		if len(fgs) == 0 {
			fmt.Println("empty config")
			os.Exit(2)
		}

		for i := range fgs {
			go handle(&fgs[i])
		}
	} else {
		if *listenPort == "" || *forwardPort == "" {
			fmt.Println("listen or forward is empty")
			os.Exit(4)
		}

		fg := &ForwardGroup{
			Listen:  *listenPort,
			Forward: *forwardPort,
		}

		go handle(fg)
	}

	for {

	}
}

func readConfig(path string) ([]ForwardGroup, error) {
	raw, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var fgs []ForwardGroup
	err = json.Unmarshal(raw, &fgs)
	if err != nil {
		return nil, err
	}

	return fgs, nil
}
func handle(fg *ForwardGroup) {
	// Process fg forward
	forwardGroup := strings.Split(fg.Forward, ":")
	if len(forwardGroup) < 2 {
		fmt.Printf("Invalid forward: %v", fg.Forward)
		return
	}
	fg.ForwardHost, fg.ForwardPort = forwardGroup[0], forwardGroup[1]

	listenGroup := strings.Split(fg.Listen, ":")
	if len(listenGroup) < 2 {
		fmt.Printf("Invalid listen: %v", fg.Listen)
		return
	}
	fg.ListenHost, fg.ListenPort = listenGroup[0], listenGroup[1]

	switch fg.Proto {
	case "tcp":
		handleTCP(fg)
	case "udp":
		handleUDP(fg)
	default:
		go handleTCP(fg)
		handleUDP(fg)

	}
}

func handleTCP(fg *ForwardGroup) {
	// Start listening local
	ln, err := net.Listen("tcp", fg.Listen)
	if err != nil {
		fmt.Printf("Listen tcp port error: %v\n", err)
		os.Exit(8)
	}

	fmt.Printf("Forwarding TCP: [%v] - [%v]\n", fg.Listen, fg.Forward)
	go watch(fg)

	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Printf("Accept error: %v\n", err)
			continue
		}

		go handleConnection(conn, fg.ForwardHost, fg.ForwardPort)
	}
}

func handleUDP(fg *ForwardGroup) {
	// var ip net.IP
	// if fg.ListenHost != "" {
	// 	ip = net.ParseIP(fg.ListenHost)
	// } else {
	// 	ip = net.ParseIP("0.0.0.0")
	// }

	// port, err := strconv.Atoi(fg.ListenPort)
	// if err != nil {
	// 	fmt.Printf("Prase udp port error: %v\n", err)
	// 	os.Exit(16)
	// }

	// ln, err := net.ListenUDP("udp", &net.UDPAddr{IP: ip,Port: })
	ln, err := net.ListenPacket("udp", fg.Listen)
	if err != nil {
		fmt.Printf("Listen udp port error: %v\n", err)
		os.Exit(8)
	}
	fmt.Printf("Forwarding UDP: [%v] - [%v]\n", fg.Listen, fg.Forward)

	for {
		ln.SetDeadline(time.Now().Add(1 * time.Second))
		p := make([]byte, 1024)
		_, conn, err := ln.ReadFrom(p)
		if err != nil {
			if !(err.(net.Error)).Timeout() {
				fmt.Printf("Accept error: %v\n", err)
			}
			continue
		}

		go handleConnectionUDP(ln, conn, p, fg.ForwardHost, fg.ForwardPort)
	}
}

func forward(src, dest net.Conn) {
	defer src.Close()
	defer dest.Close()

	ch := make(chan int, 1)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := io.Copy(src, dest)
		if err != nil {
			// fmt.Printf("IOCopy error: %v\n", err)
			return
		}
	}()

	go func() {
		defer wg.Done()
		_, err := io.Copy(dest, src)
		if err != nil {
			// fmt.Printf("IOCopy error: %v\n", err)
			return
		}
	}()

	go func() {
		wg.Wait()
		ch <- 1
	}()

	select {
	case <-ch:
		fmt.Println("IOCopy exit")
	case <-time.After(1200 * time.Second):
		fmt.Println("IOCopy timeout")
	}
}

func handleConnection(c net.Conn, forwardHost, forwardPort string) {
	forwardString := fmt.Sprintf("%v:%v", forwardHost, forwardPort)
	fmt.Printf("TCP Connection: [%v]-[%v]\n", c.RemoteAddr(), forwardString)

	remote, err := net.Dial("tcp", forwardString)
	if err != nil {
		c.Close()
		fmt.Printf("Connect to remote error: %v\n", err)
		return
	}

	go forward(c, remote)
}

func handleConnectionUDP(ln net.PacketConn, c net.Addr, p []byte, forwardHost, forwardPort string) {
	forwardString := fmt.Sprintf("%v:%v", forwardHost, forwardPort)
	fmt.Printf("UDP Packet: [%v]-[%v]\n", c.String(), forwardString)

	remote, err := net.Dial("udp", forwardString)
	if err != nil {
		fmt.Printf("Connect to remote error: %v\n", err)
		return
	}
	remote.SetDeadline(time.Now().Add(15 * time.Second))

	_, err = remote.Write(p)
	if err != nil {
		fmt.Printf("Write to remote error: %v\n", remote.RemoteAddr().String())
		return
	}

	pr := make([]byte, 1024)
	_, err = remote.Read(pr)
	if err != nil {
		fmt.Printf("Read from remote error: %v\n", remote.RemoteAddr().String())
		return
	}

	ln.WriteTo(pr, c)
}

func watch(fg *ForwardGroup) {
	for {
		if net.ParseIP(fg.ForwardHost) != nil {
			time.Sleep(60 * time.Second)
			continue
		}

		ips, err := net.LookupIP(fg.ForwardHost)
		if err != nil {
			fmt.Printf("Dns resolve error: %v\n", err)
			time.Sleep(5 * time.Second)
			continue
		}

		fg.ForwardHost = ips[0].String()
		time.Sleep(1 * time.Second)
	}
}
