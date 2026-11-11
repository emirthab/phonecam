package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"net"
	"sync"
	"time"
)

const (
	outW      = 1280
	outH      = 720
	frameSize = outW * outH * 3 / 2
	feedAddr  = "127.0.0.1:47811"
)

// hub keeps the latest NV12 frame and fans it out to virtual camera clients.
type hub struct {
	mu      sync.Mutex
	frame   []byte
	seq     uint64
	last    time.Time
	subs    map[chan struct{}]struct{}
	count   int
	fps     int
	clients int
}

func newHub() *hub {
	h := &hub{subs: map[chan struct{}]struct{}{}}
	go func() {
		for range time.Tick(time.Second) {
			h.mu.Lock()
			h.fps, h.count = h.count, 0
			h.mu.Unlock()
		}
	}()
	return h
}

type hubStatus struct {
	phoneConnected bool
	fps            int
	clients        int // virtual camera consumers (frame server connections)
}

func (h *hub) status() hubStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	return hubStatus{
		phoneConnected: !h.last.IsZero() && time.Since(h.last) < 2*time.Second,
		fps:            h.fps,
		clients:        h.clients,
	}
}

func (h *hub) publishJPEG(data []byte) error {
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	buf := make([]byte, frameSize)
	toNV12(img, buf)

	h.mu.Lock()
	h.frame = buf
	h.seq++
	h.last = time.Now()
	h.count++
	for ch := range h.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	h.mu.Unlock()
	return nil
}

// serveFeed serves raw frames to the virtual camera DLL over loopback.
func (h *hub) serveFeed() error {
	ln, err := net.Listen("tcp", feedAddr)
	if err != nil {
		return err
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				log.Printf("feed accept: %v", err)
				time.Sleep(time.Second)
				continue
			}
			go h.feedConn(c)
		}
	}()
	return nil
}

func (h *hub) feedConn(c net.Conn) {
	defer c.Close()
	if tc, ok := c.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
		tc.SetWriteBuffer(4 << 20)
	}
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.clients++
	h.mu.Unlock()
	log.Printf("virtual camera connected (%s)", c.RemoteAddr())
	defer func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.clients--
		h.mu.Unlock()
		log.Printf("virtual camera disconnected")
	}()

	hdr := make([]byte, 16)
	copy(hdr, "PCAM")
	binary.LittleEndian.PutUint32(hdr[4:], outW)
	binary.LittleEndian.PutUint32(hdr[8:], outH)
	binary.LittleEndian.PutUint32(hdr[12:], frameSize)

	var sentSeq uint64
	ch <- struct{}{} // send the current frame right away
	for range ch {
		h.mu.Lock()
		frame, seq, last := h.frame, h.seq, h.last
		h.mu.Unlock()
		if frame == nil || seq == sentSeq || time.Since(last) > 2*time.Second {
			continue
		}
		sentSeq = seq
		c.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err := c.Write(hdr); err != nil {
			return
		}
		if _, err := c.Write(frame); err != nil {
			return
		}
	}
}

var lutY, lutC [256]byte

func init() {
	// JPEG is full range; NV12 for cameras is limited (studio) range.
	for i := 0; i < 256; i++ {
		lutY[i] = byte(16 + (i*219+127)/255)
		lutC[i] = byte(128 + ((i-128)*224)/255)
	}
}

// toNV12 letterboxes img into a 1280x720 NV12 buffer.
func toNV12(img image.Image, dst []byte) {
	yp := dst[:outW*outH]
	uv := dst[outW*outH:]
	for i := range yp {
		yp[i] = 16
	}
	for i := range uv {
		uv[i] = 128
	}

	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw < 2 || sh < 2 {
		return
	}
	dw, dh := outW, outH
	if sw*outH > sh*outW {
		dh = sh * outW / sw
	} else {
		dw = sw * outH / sh
	}
	dw &^= 1
	dh &^= 1
	if dw < 2 || dh < 2 {
		return
	}
	ox := ((outW - dw) / 2) &^ 1
	oy := ((outH - dh) / 2) &^ 1

	// source coordinate (16.16 fixed point) of each destination pixel center
	srcPos := func(d, dn, sn int) int {
		p := ((2*d+1)*sn<<16)/(2*dn) - (1 << 15)
		if p < 0 {
			p = 0
		}
		if max := (sn - 1) << 16; p > max {
			p = max
		}
		return p
	}

	if src, ok := img.(*image.YCbCr); ok {
		xs0 := make([]int, dw)
		xs1 := make([]int, dw)
		xf := make([]int, dw)
		for x := 0; x < dw; x++ {
			p := srcPos(x, dw, sw)
			xs0[x] = p >> 16
			xs1[x] = min(xs0[x]+1, sw-1)
			xf[x] = p & 0xffff
		}
		for y := 0; y < dh; y++ {
			p := srcPos(y, dh, sh)
			sy0 := p >> 16
			sy1 := min(sy0+1, sh-1)
			fy := p & 0xffff
			r0 := src.Y[sy0*src.YStride:]
			r1 := src.Y[sy1*src.YStride:]
			out := yp[(oy+y)*outW+ox:]
			for x := 0; x < dw; x++ {
				a, bb := int(r0[xs0[x]]), int(r0[xs1[x]])
				c, d := int(r1[xs0[x]]), int(r1[xs1[x]])
				fx := xf[x]
				top := a + ((bb-a)*fx)>>16
				bot := c + ((d-c)*fx)>>16
				out[x] = lutY[top+((bot-top)*fy)>>16]
			}
		}
		for cy := 0; cy < dh/2; cy++ {
			sy := ((2*cy+1)*sh)/dh + b.Min.Y
			if sy >= b.Max.Y {
				sy = b.Max.Y - 1
			}
			out := uv[((oy/2)+cy)*outW+ox:]
			for cx := 0; cx < dw/2; cx++ {
				sx := ((2*cx+1)*sw)/dw + b.Min.X
				if sx >= b.Max.X {
					sx = b.Max.X - 1
				}
				ci := src.COffset(sx, sy)
				out[2*cx] = lutC[src.Cb[ci]]
				out[2*cx+1] = lutC[src.Cr[ci]]
			}
		}
		return
	}

	// Generic (slow) path for non-YCbCr JPEGs, e.g. grayscale.
	for y := 0; y < dh; y++ {
		sy := b.Min.Y + y*sh/dh
		for x := 0; x < dw; x++ {
			sx := b.Min.X + x*sw/dw
			c := color.YCbCrModel.Convert(img.At(sx, sy)).(color.YCbCr)
			yp[(oy+y)*outW+ox+x] = lutY[c.Y]
			if x%2 == 0 && y%2 == 0 {
				i := ((oy+y)/2)*outW + ox + x
				uv[i] = lutC[c.Cb]
				uv[i+1] = lutC[c.Cr]
			}
		}
	}
}
