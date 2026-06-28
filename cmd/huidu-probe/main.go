package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	huidu "github.com/alparslanahmed/huidu-led"
)

func main() {
	host := flag.String("host", "", "controller IP address")
	port := flag.Int("port", huidu.DefaultPort, "controller port")
	timeout := flag.Duration("timeout", 2*time.Second, "probe timeout")
	flag.Parse()

	if *host == "" {
		fmt.Fprintln(os.Stderr, "missing required -host")
		os.Exit(2)
	}

	result := huidu.ProbeProtocol(*host, *port, *timeout)
	fmt.Printf("host=%s port=%d\n", result.Host, result.Port)
	printTransport("tcp", result.TCP)
	printTransport("udp", result.UDP)
}

func printTransport(name string, result huidu.ProtocolProbeTransport) {
	fmt.Printf("%s reachable=%t protocol=%s", name, result.Reachable, result.Protocol)
	if result.RemoteAddr != "" {
		fmt.Printf(" remote=%s", result.RemoteAddr)
	}
	if result.Error != "" {
		fmt.Printf(" error=%q", result.Error)
	}
	fmt.Println()
	if len(result.Response) > 0 {
		fmt.Printf("%s response=% x\n", name, result.Response)
	}
}
