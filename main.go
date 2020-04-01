package main

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/mdlayher/raw"
	"github.com/miekg/pcap"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type Frame struct {
	FrameID   uint16
	LastChunk uint16
	Data      []byte
}

func main() {
	interf := flag.String("interface", "eth0", "What interface the device is attached to")
	debug := flag.Bool("debug", false, "Print loads of debug info")
	output_mkv := flag.Bool("mkv", true, "Spit out Audio + Video contained in MKV, else spit out raw MJPEG")
	audio := flag.Bool("audio", true, "Output audio into MKV as well")
	wakeup := flag.Bool("wakeups", true, "Send packets needed to start/keep the sender transmitting")
	sendermac := flag.String("sender-mac", "000b78006001", "The macaddress of the sender unit")
	flag.Parse()

	var videowriter *os.File
	pipename := randString(5)
	audiodis := make(chan []byte, 100)
	videodis := make(chan []byte, 100)

	if *wakeup {
		go BroadcastWakeups(*interf, *sendermac)
	}

	if *output_mkv {
		go WrapinMKV(fmt.Sprintf("/tmp/hdmi-Vfifo-%s", pipename), audiodis, *audio)

		err := syscall.Mkfifo(fmt.Sprintf("/tmp/hdmi-Vfifo-%s", pipename), 0664)
		if err != nil {
			log.Fatalf("Could not make a fifo in /tmp/hdmi-Vfifo-%s, %s", pipename, err.Error())
		}

		videowriter, err = os.OpenFile(fmt.Sprintf("/tmp/hdmi-Vfifo-%s", pipename), os.O_WRONLY, 0664)
		if err != nil {
			log.Fatalf("Could not open newly made fifo in /tmp/hdmi-Vfifo-%s, %s", pipename, err.Error())
			return
		}
	} else {
		videowriter = os.Stdout
	}

	go DumpChanToFile(videodis, videowriter)

	MULTICAST_MAC := []byte{0x01, 0x00, 0x5e, 0x02, 0x02, 0x02}

	h, err := pcap.OpenLive(*interf, 1500, true, 500)
	if h == nil {
		fmt.Fprintf(os.Stderr, "de hdmi: %s\n", err)
		return
	}
	defer h.Close()

	droppedframes := 0
	desyncframes := 0
	totalframes := 0

	CurrentPacket := Frame{}
	CurrentPacket.Data = make([]byte, 0)

	videodis <- []byte("--myboundary\nContent-Type: image/jpeg\n\n")

	for pkt, r := h.NextEx(); r >= 0; pkt, r = h.NextEx() {
		if r == 0 {
			// Timeout, continue
			continue
		}

		MACADDR := pkt.Data[0:6]
		if bytes.Compare(MACADDR, MULTICAST_MAC) != 0 {
			// This isnt the multicast packet we are looking for
			continue
		}

		// Ethernet + IP + UDP = 50, Min packet size is 5 bytes, thus 55
		if len(pkt.Data) < 100 {
			continue
		}

		ApplicationData := pkt.Data[42:]

		// Maybe there is some audio data on port 2066?
		if pkt.Data[36] == 0x08 && pkt.Data[37] == 0x12 && *output_mkv && *audio {
			select {
			case audiodis <- ApplicationData[16:]:
			default:
			}

			continue
		}

		// Check that the port is 2068
		if pkt.Data[36] != 0x08 || pkt.Data[37] != 0x14 {
			continue
		}

		FrameNumber := uint16(0)
		CurrentChunk := uint16(0)

		buf := bytes.NewBuffer(ApplicationData[:2])
		buf2 := bytes.NewBuffer(ApplicationData[2:4])
		binary.Read(buf, binary.BigEndian, &FrameNumber)
		binary.Read(buf2, binary.BigEndian, &CurrentChunk)

		if CurrentPacket.FrameID != FrameNumber && CurrentPacket.FrameID != 0 {
			// Did we drop a packet ?
			droppedframes++
			if CurrentPacket.FrameID < FrameNumber {
				CurrentPacket = Frame{}
				CurrentPacket.Data = make([]byte, 0)
				CurrentPacket.LastChunk = 0
				log.Printf("Dropped packet because of non sane frame number (%d dropped so far)", droppedframes)
			}
			continue
		}

		if *debug {
			log.Printf("%d/%d - %d/%d - %d", FrameNumber, CurrentChunk, CurrentPacket.FrameID, CurrentPacket.LastChunk, len(ApplicationData))
		}

		if CurrentPacket.LastChunk != 0 && CurrentPacket.LastChunk != CurrentChunk-1 {
			if uint16(^(CurrentChunk << 15)) != 65534 {
				log.Printf("Dropped packet because of desync detected (%d dropped so far, %d because of desync)",
					droppedframes, desyncframes)

				log.Printf("You see; %d != %d-1",
					CurrentPacket.LastChunk, CurrentChunk)

				// Oh dear, we are out of sync, Drop the frame
				droppedframes++
				desyncframes++
				CurrentPacket = Frame{}
				CurrentPacket.Data = make([]byte, 0)
				CurrentPacket.LastChunk = 0

				continue
			}
			CurrentPacket.LastChunk = CurrentChunk
		}

		CurrentPacket.Data = append(CurrentPacket.Data, ApplicationData[4:]...)

		if uint16(^(CurrentChunk >> 15)) == 65534 {
			// Flush the frame to output

			fin := []byte("\n--myboundary\nContent-Type: image/jpeg\n\n")
			fin = append(fin, CurrentPacket.Data...)
			select {
			case videodis <- fin:
			default:
			}

			totalframes++

			if *debug {
				log.Printf("Size: %d", len(CurrentPacket.Data))
			}

			CurrentPacket = Frame{}
			CurrentPacket.Data = make([]byte, 0)
			CurrentPacket.FrameID = 0
			CurrentPacket.LastChunk = 0
		}

	}
}

func randString(n int) string {
	const alphanum = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	var bytes = make([]byte, n)
	rand.Read(bytes)
	for i, b := range bytes {
		bytes[i] = alphanum[b%byte(len(alphanum))]
	}
	return string(bytes)
}

func WrapinMKV(uuidpath string, audioin chan []byte, audio bool) {
	var ffmpeg *exec.Cmd
	if audio {
		ffmpeg = exec.Command("ffmpeg", "-f", "mjpeg", "-i", uuidpath, "-f", "s32be", "-ac", "2", "-ar", "44100", "-i", "pipe:0", "-f", "matroska", "-codec", "copy", "pipe:1")
	} else {
		ffmpeg = exec.Command("ffmpeg", "-f", "mjpeg", "-i", uuidpath, "-f", "matroska", "-codec", "copy", "pipe:1")
	}
	ffmpegstdout, err := ffmpeg.StdoutPipe()
	if err != nil {
		log.Fatalf("Unable to setup pipes for ffmpeg (stdout)")
	}
	ffmpeg.Stderr = os.Stderr

	audiofile, err := ffmpeg.StdinPipe()

	go DumpChanToFile(audioin, audiofile)

	ffmpeg.Start()

	for {
		_, err := io.Copy(os.Stdout, ffmpegstdout)
		if err != nil {
			log.Fatalf("unable to read to stdout: %s", err.Error())
		}
	}
}

func DumpChanToFile(channel chan []byte, file io.WriteCloser) {
	for blob := range channel {
		buf := bytes.NewBuffer(blob)
		_, err := io.Copy(file, buf)
		if err != nil {
			log.Fatalf("unable to write to pipe: %s", err.Error())
		}
	}

	log.Fatalf("Channel closed")
}

func BroadcastWakeups(ifname string, sendermac string) {
	ifc, err := net.InterfaceByName(ifname)
	if err != nil {
		log.Fatalf("Unable get the interface name of %s, %s", ifname, err.Error())
	}

	macbytes, err := hex.DecodeString(sendermac)

	if err != nil {
		log.Fatalf("Invalid MAC address string %s , %s", sendermac, err.Error())
	}

	packet := append([]byte{
		0x0b, 0x00, 0x0b, 0x78, 0x00, 0x60, 0x02, 0x90, 0x2b, 0x34, 0x31, 0x02, 0x08, 0x00, 0x45, 0xfc,
		0x02, 0x1c, 0x00, 0x0a, 0x00, 0x00, 0x40, 0x11, 0xa6, 0x0a, 0xc0, 0xa8, 0xa8, 0x38, 0xc0, 0xa8,
		0xa8, 0x37, 0xbe, 0x31, 0xbe, 0x31, 0x02, 0x08, 0xd6, 0xdc, 0x54, 0x46, 0x36, 0x7a, 0x60, 0x02,
		0x00, 0x00, 0x0a, 0x00, 0x00, 0x03, 0x03, 0x01, 0x00, 0x26, 0x00, 0x00, 0x00, 0x00, 0x02, 0xef,
		0xdc}, make([]byte, 489)...)

	packet = append(macbytes[:6], packet[6:]...)

	for {
		conn, err := raw.ListenPacket(ifc, raw.ProtocolARP)
		if err != nil {
			log.Fatalf("Unable to keep broadcasting the keepalives, %s", err.Error())
		}
		conn.WriteTo(packet, &raw.Addr{HardwareAddr: macbytes})
		conn.Close()
		time.Sleep(time.Second)
	}
}
