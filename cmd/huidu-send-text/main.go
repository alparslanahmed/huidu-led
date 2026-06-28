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
	width := flag.Int("width", 0, "screen width; 0 uses SendText default")
	height := flag.Int("height", 0, "screen height; 0 uses SendText default")
	color := flag.String("color", huidu.ColorWhite, "text color (#RRGGBB)")
	bg := flag.String("bg", "", "background color (#RRGGBB)")
	timeout := flag.Duration("timeout", 5*time.Second, "network timeout")
	flag.Parse()

	dev := huidu.NewDevice(*host, *port, huidu.WithTimeout(*timeout), huidu.WithLogger(log.Default()))
	if err := dev.Connect(); err != nil {
		fmt.Fprintf(os.Stderr, "connect failed: %v\n", err)
		os.Exit(1)
	}
	defer dev.Close()

	var err error
	if *width > 0 && *height > 0 {
		screen := huidu.NewScreen()
		program := screen.AddProgram("test")
		area := program.AddFullScreenArea(*width, *height)
		area.AddText(*text, huidu.TextConfig{Color: *color, BackgroundColor: *bg, HAlign: huidu.HAlignCenter, VAlign: huidu.VAlignMiddle})
		err = dev.SendScreen(screen)
	} else {
		err = dev.SendText(*text, huidu.TextConfig{Color: *color, BackgroundColor: *bg})
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "send failed: %v\n", err)
		os.Exit(1)
	}

	if cardType, ok := dev.HD2020CardType(); ok {
		fmt.Printf("sent text %q to %s:%d using protocol %s cardType=0x%02x\n", *text, *host, *port, dev.Protocol(), cardType)
		return
	}
	fmt.Printf("sent text %q to %s:%d using protocol %s\n", *text, *host, *port, dev.Protocol())
}
