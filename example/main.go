package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"
	"path/filepath"

	"github.com/ur65/go-ico"
)

var (
	outdir string
)

func init() {
	flag.StringVar(&outdir, "o", ".", "output directory")
	flag.Usage = func() {
		fmt.Println("usage: go run main.go [-o OUTDIR] ICO_FILE")
		fmt.Println("example: go run main.go -o ./out ./sample.ico")
		os.Exit(2)
	}
}

func run() error {
	flag.Parse()
	args := flag.Args()
	if len(args) != 1 {
		flag.Usage()
	}
	icopath := args[0]

	if err := os.MkdirAll(outdir, 0755); err != nil {
		return err
	}

	f, err := os.Open(icopath)
	if err != nil {
		return err
	}
	defer f.Close()

	imgs, err := ico.Decode(f)
	if err != nil {
		return err
	}

	base := filepath.Base(icopath[:len(icopath)-len(filepath.Ext(icopath))])
	for i, v := range imgs {
		f, err := os.Create(filepath.Join(outdir, fmt.Sprintf("%s%02d.png", base, i+1)))
		if err != nil {
			return err
		}
		defer f.Close()
		if err := png.Encode(f, v); err != nil {
			return err
		}
	}

	return nil
}

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}
