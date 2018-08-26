package main

// The purpose of this program, is to test broadcast input from Hub to App
import (
	"log"
	"net"
	"time"

	reuse "github.com/libp2p/go-reuseport"
	rwf "github.com/pdxiv/gonetworktest"
)

func main() {
	startSession()
}

func startSession() {
	// Load configuration from file
	configuration := rwf.GetConfiguration(rwf.ConfigFile)

	// Listen to incoming UDP datagrams
	pc, err := reuse.ListenPacket("udp", configuration.AppSinkAddress)
	defer pc.Close()
	if err != nil {
		log.Fatal(err)
	}

	// Initialize time ticker for keeping track of when events happen
	ticker := time.NewTicker(time.Nanosecond)
	defer ticker.Stop()
	latestTime := time.Now().UnixNano() // Initialize timestamp

	// Initialize channel for receiving
	appReceiver := make(chan rwf.AppCommData, 1)

	go receiveHubMessage(pc, appReceiver)
	for {
		select {
		case t := <-ticker.C:
			latestTime = t.UnixNano()
		case messageReceived := <-appReceiver:
			log.Print("Message: ", string(messageReceived.Payload), " Time: ", latestTime)
		}
	}
}

func receiveHubMessage(pc net.PacketConn, appReceiver chan rwf.AppCommData) {
	var hubData rwf.HubCommData
	rwf.InitHubMessage(&hubData)
	var appData rwf.AppCommData
	rwf.InitAppMessage(&appData)
	hubData.MasterBuffer = hubData.MasterBuffer[0:rwf.BufferAllocationSize] // allocate receive buffer

	for {
		// Simple read
		pc.ReadFrom(hubData.MasterBuffer)
		if rwf.DecodeHubMessage(&hubData) {
			// Copy the payload of the hub message to the Master Buffer of the app message
			appData.MasterBuffer = hubData.Payload
			rwf.AppDecodeAppMessage(&appData)
			appReceiver <- appData
			hubData.ExpectedHubSequenceNumber++
		} else {
		}
	}
}