package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"github.com/miekg/pcap"
	"io"
	"log"
	"os"
)

type Frame struct {
	FrameID   uint16
	LastChunk uint16
	Data      []byte
}

func main() {
	interf := flag.String("interface", "eth0", "What interface the device is attached to")
	flag.Parse()

	MULTICAST_MAC := []byte{0x01, 0x00, 0x5e, 0x02, 0x02, 0x02}

	h, err := pcap.OpenLive(*interf, 1500, true, 500)
	if h == nil {
		fmt.Fprintf(os.Stderr, "de hdmi: %s\n", err)
		return
	}
	defer h.Close()

	droppedframes := 0
	desyncframes := 0

	CurrentPacket := Frame{}
	CurrentPacket.Data = make([]byte, 0)

	os.Stdout.WriteString("--myboundary\nContent-Type: image/jpeg\n\n")

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

		// Check that the port is 2068
		if pkt.Data[34] != 0x08 || pkt.Data[35] != 0x14 {
			continue
		}

		ApplicationData := pkt.Data[42:]

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
				CurrentPacket.FrameID = CurrentPacket.FrameID
				CurrentPacket.LastChunk = 0
				log.Printf("Dropped packet because of non sane frame number (%d dropped so far)", droppedframes)
			}
			continue
		}

		// log.Printf("%d/%d - %d/%d - %d", FrameNumber, CurrentChunk, CurrentPacket.FrameID, CurrentPacket.LastChunk, len(ApplicationData))

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
				CurrentPacket.FrameID = CurrentPacket.FrameID
				CurrentPacket.LastChunk = 0

				continue
			}
			CurrentPacket.LastChunk = CurrentChunk
		}

		for _, v := range ApplicationData[4:] {
			CurrentPacket.Data = append(CurrentPacket.Data, v)
		}

		if uint16(^(CurrentChunk >> 15)) == 65534 {
			// Flush the frame to output
			os.Stdout.WriteString("\n--myboundary\nContent-Type: image/jpeg\n\n")
			log.Printf("Size: %d", len(CurrentPacket.Data))
			buf := bytes.NewBuffer(CurrentPacket.Data)
			io.Copy(os.Stdout, buf)

			CurrentPacket = Frame{}
			CurrentPacket.Data = make([]byte, 0)
			CurrentPacket.FrameID = 0
			CurrentPacket.LastChunk = 0
		}

	}
}
