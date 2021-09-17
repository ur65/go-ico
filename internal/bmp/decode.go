package bmp

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
)

const (
	fileHeaderSize = 14
	infoHeaderSize = 40
)

type fileHeader struct {
	Signature  uint16
	FileSize   uint32
	Reserved1  uint16
	Reserved2  uint16
	OffsetBits uint32
}

func readFileHeader(r io.Reader) (fileHeader, error) {
	h := fileHeader{}
	if err := binary.Read(r, binary.LittleEndian, &h); err != nil {
		return fileHeader{}, err
	}

	sig := make([]byte, 2)
	binary.LittleEndian.PutUint16(sig, h.Signature)

	if string(sig) != "BM" {
		return fileHeader{}, fmt.Errorf("bmp: file signature should be 'BM' (got: %q)", sig)
	}

	return h, nil
}

type infoHeaderWithoutSize struct {
	Width          int32
	Height         int32
	Planes         uint16
	BitCount       uint16
	Compression    uint32
	SizeImage      uint32
	XPelsPerMeter  int32
	YPelsPerMeter  int32
	ColorUsed      uint32
	ColorImportant uint32
}

type InfoHeader struct {
	Size uint32
	infoHeaderWithoutSize
}

func ReadInfoHeader(r io.Reader) (InfoHeader, error) {
	var size uint32
	if err := binary.Read(r, binary.LittleEndian, &size); err != nil {
		return InfoHeader{}, err
	}

	ihws := infoHeaderWithoutSize{}
	if err := binary.Read(r, binary.LittleEndian, &ihws); err != nil {
		return InfoHeader{}, err
	}

	h := InfoHeader{
		Size:                  size,
		infoHeaderWithoutSize: ihws,
	}

	if h.Width <= 0 {
		return InfoHeader{}, fmt.Errorf("bmp: width should be greater than zero (got: %d)", h.Width)
	}

	if h.Height == 0 {
		return InfoHeader{}, fmt.Errorf("bmp: height should be non-zero (got: %d)", h.Height)
	}

	return h, nil
}

// colorBGR is BGR order
type colorBGR struct {
	B        uint8
	G        uint8
	R        uint8
	Reserved uint8
}

func (c colorBGR) RGBA() color.RGBA {
	return color.RGBA{c.R, c.G, c.B, 0xff}
}

type decoder struct {
	bpp        int
	isOpposite bool
	config     image.Config
}

func newDecoder(r io.Reader) (*decoder, error) {
	_, err := readFileHeader(r)
	if err != nil {
		return nil, err
	}

	ih, err := ReadInfoHeader(r)
	if err != nil {
		return nil, err
	}

	switch ih.Size {
	case 40, 108, 124:
	default:
		return nil, fmt.Errorf("bmp: unsupported DIB header size (got: %d)", ih.Size)
	}

	var isOpposite bool
	if ih.Height < 0 {
		ih.Height *= -1
		isOpposite = true
	}

	if ih.Compression != 0 {
		return nil, fmt.Errorf("bmp: supported compression method is only 0 (got: %d)", ih.Compression)
	}

	if ih.ColorUsed == 0 {
		ih.ColorUsed = 1 << ih.BitCount
	}

	var model color.Model

	switch ih.BitCount {
	case 1, 4, 8:
		clrs := make([]colorBGR, ih.ColorUsed)
		if err := binary.Read(r, binary.LittleEndian, &clrs); err != nil {
			return nil, err
		}
		colorTable := make(color.Palette, ih.ColorUsed)
		for i := range colorTable {
			colorTable[i] = clrs[i].RGBA()
		}
		model = colorTable
	case 16, 24, 32:
		model = color.RGBAModel
	default:
		return nil, fmt.Errorf("bmp: unsupported bpp (got: %d)", ih.BitCount)
	}

	c := image.Config{ColorModel: model, Width: int(ih.Width), Height: int(ih.Height)}

	d := &decoder{
		bpp:        int(ih.BitCount),
		isOpposite: isOpposite,
		config:     c,
	}

	return d, nil
}

func (d *decoder) decode1(r io.Reader) (image.Image, error) {
	w, h := d.config.Width, d.config.Height
	paletted := image.NewPaletted(image.Rect(0, 0, w, h), d.config.ColorModel.(color.Palette))

	y0, y1, dy := h-1, -1, -1
	if d.isOpposite {
		y0, y1, dy = 0, h, 1
	}

	// row data must be an integer multiple of 4 bytes
	row := make([]byte, (w*d.bpp+31)/32*4)
	for y := y0; y != y1; y += dy {
		if _, err := io.ReadFull(r, row); err != nil {
			if err == io.EOF {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}

		p := paletted.Pix[y*paletted.Stride : (y+1)*paletted.Stride]

		for i := 0; i < (w+1)/8; i++ {
			p[i*8+0] = (row[i] & 0x80) >> 7
			p[i*8+1] = (row[i] & 0x40) >> 6
			p[i*8+2] = (row[i] & 0x20) >> 5
			p[i*8+3] = (row[i] & 0x10) >> 4
			p[i*8+4] = (row[i] & 0x08) >> 3
			p[i*8+5] = (row[i] & 0x04) >> 2
			p[i*8+6] = (row[i] & 0x02) >> 1
			p[i*8+7] = (row[i] & 0x01)
		}
	}

	return paletted, nil
}

func (d *decoder) decode4(r io.Reader) (image.Image, error) {
	w, h := d.config.Width, d.config.Height
	paletted := image.NewPaletted(image.Rect(0, 0, w, h), d.config.ColorModel.(color.Palette))

	y0, y1, dy := h-1, -1, -1
	if d.isOpposite {
		y0, y1, dy = 0, h, 1
	}

	// row data must be an integer multiple of 4 bytes
	row := make([]byte, (w*d.bpp+31)/32*4)
	for y := y0; y != y1; y += dy {
		if _, err := io.ReadFull(r, row); err != nil {
			if err == io.EOF {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}

		p := paletted.Pix[y*paletted.Stride : (y+1)*paletted.Stride]

		for i := 0; i < (w+1)/2; i++ {
			p[i*2+0] = (row[i] & 0xf0) >> 4
			p[i*2+1] = (row[i] & 0xf)
		}
	}

	return paletted, nil
}

func (d *decoder) decode8(r io.Reader) (image.Image, error) {
	w, h := d.config.Width, d.config.Height
	paletted := image.NewPaletted(image.Rect(0, 0, w, h), d.config.ColorModel.(color.Palette))

	y0, y1, dy := h-1, -1, -1
	if d.isOpposite {
		y0, y1, dy = 0, h, 1
	}

	// row data must be an integer multiple of 4 bytes
	row := make([]byte, (w*d.bpp+31)/32*4)
	for y := y0; y != y1; y += dy {
		if _, err := io.ReadFull(r, row); err != nil {
			if err == io.EOF {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}

		p := paletted.Pix[y*paletted.Stride : (y+1)*paletted.Stride]

		copy(p, row[:w])
	}

	return paletted, nil
}

func (d *decoder) decode24(r io.Reader) (image.Image, error) {
	w, h := d.config.Width, d.config.Height
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))

	y0, y1, dy := h-1, -1, -1
	if d.isOpposite {
		y0, y1, dy = 0, h, 1
	}

	row := make([]byte, (w*3+3)&^3)
	for y := y0; y != y1; y += dy {
		if _, err := io.ReadFull(r, row); err != nil {
			if err == io.EOF {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}

		p := rgba.Pix[y*rgba.Stride : (y+1)*rgba.Stride]
		for i, j := 0, 0; i < w*4; i, j = i+4, j+3 {
			// BGRA order
			p[i+0] = row[j+2]
			p[i+1] = row[j+1]
			p[i+2] = row[j+0]
			p[i+3] = 0xff
		}
	}

	return rgba, nil
}

func (d *decoder) decode32(r io.Reader) (image.Image, error) {
	w, h := d.config.Width, d.config.Height
	rgba := image.NewNRGBA(image.Rect(0, 0, w, h))

	y0, y1, dy := h-1, -1, -1
	if d.isOpposite {
		y0, y1, dy = 0, h, 1
	}

	for y := y0; y != y1; y += dy {
		p := rgba.Pix[y*rgba.Stride : (y+1)*rgba.Stride]

		if _, err := io.ReadFull(r, p[:rgba.Stride]); err != nil {
			if err == io.EOF {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}

		for i := 0; i < w*4; i += 4 {
			// BGRA order
			p[i], p[i+2] = p[i+2], p[i]
		}
	}

	return rgba, nil
}

func (d *decoder) decode(r io.Reader) (image.Image, error) {
	switch d.bpp {
	case 1:
		return d.decode1(r)
	case 4:
		return d.decode4(r)
	case 8:
		return d.decode8(r)
	case 24:
		return d.decode24(r)
	case 32:
		return d.decode32(r)
	}

	return nil, fmt.Errorf("bmp: the bpp decode fucntion isn't implemented (got: %d)", d.bpp)
}

// Decode reads a BMP image form io.Reader and returns an image.Image
func Decode(r io.Reader) (image.Image, error) {
	d, err := newDecoder(r)
	if err != nil {
		return nil, err
	}

	img, err := d.decode(r)
	if err != nil {
		return nil, err
	}

	return img, nil
}

// DecodeConfig reads a BMP image from io.Reader and returns an image.Config
func DecodeConfig(r io.Reader) (image.Config, error) {
	d, err := newDecoder(r)
	if err != nil {
		return image.Config{}, err
	}

	return d.config, nil
}

func init() {
	image.RegisterFormat("bmp", "BM", Decode, DecodeConfig)
}
