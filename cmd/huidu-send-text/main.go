package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	huidu "github.com/alparslanahmed/huidu-led"
)

func main() {
	host := flag.String("host", "192.168.1.200", "controller host")
	port := flag.Int("port", 6101, "controller TCP port")
	text := flag.String("text", "TEST123", "text to show")
	timeout := flag.Duration("timeout", 5*time.Second, "network timeout")
	flag.Parse()

	dev := huidu.NewDevice(*host, *port, huidu.WithTimeout(*timeout), huidu.WithLogger(log.Default()))
	if err := dev.Connect(); err != nil {
		fmt.Fprintf(os.Stderr, "connect failed: %v\n", err)
		os.Exit(1)
	}
	defer dev.Close()

	if err := dev.SendText(*text, huidu.TextConfig{Color: huidu.ColorWhite}); err != nil {
		fmt.Fprintf(os.Stderr, "send failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("sent text %q to %s:%d using protocol %s\n", *text, *host, *port, dev.Protocol())
}
