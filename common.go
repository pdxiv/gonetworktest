package main

// Commonly used functions
import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
)

const PacketLimit = 100 // If we're afraid of killing our network with the amount of load
const ConfigFile = "conf.json"
const BufferAllocationSize = 65507

// For handling configuration parameters
type Configuration struct {
	SequencerSinkAddress string
	SequencerRiseAddress string
	AppSinkAddress       string
	AppRiseAddress       string
}

// For handling communication from an App to the Sequencer
type AppCommData struct {
	// Actual data as native data types
	Type                      uint16
	PayloadSize               uint16
	Id                        uint64
	AppSequenceNumber         uint64
	ExpectedAppSequenceNumber uint64
	// Temporary buffer storage for data
	TypeBuffer              []byte
	SizeBuffer              []byte
	IdBuffer                []byte
	AppSequenceNumberBuffer []byte
	Payload                 []byte
	// The actual data as bytes that will be sent over UDP
	MasterBuffer []byte
}

// For handling communication from a Sequencer to the Apps
type SeqCommData struct {
	// Actual data as native data types
	SessionId                 uint64
	SeqSequenceNumber         uint64
	NumberOfAppPayloads       uint16 // If we put together several App in one Seq
	ExpectedSeqSequenceNumber uint64
	// Temporary buffer storage for data
	SessionIdBuffer           []byte
	SeqSequenceNumberBuffer   []byte
	NumberOfAppPayloadsBuffer []byte
	Payload                   []byte
	// The actual data as bytes that will be sent over UDP
	MasterBuffer []byte
}

// Initialize all the message parameters
func initAppMessage(data *AppCommData) {
	data.Type = 0
	data.PayloadSize = 0
	data.Id = 0
	data.AppSequenceNumber = 0
	data.ExpectedAppSequenceNumber = 0
	data.TypeBuffer = make([]byte, 2)
	data.SizeBuffer = make([]byte, 2)
	data.IdBuffer = make([]byte, 8)
	data.AppSequenceNumberBuffer = make([]byte, 8)
	data.Payload = make([]byte, 0, BufferAllocationSize)
	data.MasterBuffer = make([]byte, 0, BufferAllocationSize)
}

// Initialize all the message parameters
func initSeqMessage(data *SeqCommData) {
	data.SessionId = 31337
	data.SeqSequenceNumber = 0
	data.NumberOfAppPayloads = 1 // To begin with only ever 1 App in one Seq msg
	data.ExpectedSeqSequenceNumber = 0
	data.SessionIdBuffer = make([]byte, 8)
	data.SeqSequenceNumberBuffer = make([]byte, 8)
	data.NumberOfAppPayloadsBuffer = make([]byte, 2)
	data.Payload = make([]byte, 0, BufferAllocationSize)
	data.MasterBuffer = make([]byte, 0, BufferAllocationSize)
}

// Decode the bytes in a message from a Seq
func decodeSeqMessage(data *SeqCommData) bool {
	data.SessionId = binary.BigEndian.Uint64(data.MasterBuffer[0:8])
	data.SeqSequenceNumber = binary.BigEndian.Uint64(data.MasterBuffer[8:16])
	data.NumberOfAppPayloads = binary.BigEndian.Uint16(data.MasterBuffer[16:18])
	data.Payload = data.MasterBuffer[10:]
	/*
		Here's how the gap detection should work for an App listening to Seq:
		- At initialization, set ExpectedSeqSequenceNumber to 0
		- Read the Seq message, and decode SeqSequenceNumber
		- if ExpectedSeqSequenceNumber == SeqSequenceNumber then
		-   increment ExpectedSeqSequenceNumber
		- else
		-   report a sequence number gap, and ask for the gaps to be filled
		-   ExpectedSeqSequenceNumber = SeqSequenceNumber + 1

		Seq sequence number handling should have three possible scenarios:
		- expected sequence number - continue
		- higher sequence number than expected - report gap, request lost data
		- lower sequence number than expected - do nothing
	*/

	if data.ExpectedSeqSequenceNumber < data.SeqSequenceNumber {
		// Here we should have code to fill gaps from a "gobacker"
		fmt.Println("**************** Sequence number", data.SeqSequenceNumber, "not expected. Too high. Expecting", data.ExpectedSeqSequenceNumber, "We should try to re-fetch ", data.ExpectedSeqSequenceNumber, "-", data.SeqSequenceNumber-1, "before continuing.")
		data.ExpectedSeqSequenceNumber = data.SeqSequenceNumber + 1 // Just continue without missing data, for now
		return false
	} else if data.ExpectedSeqSequenceNumber != data.SeqSequenceNumber {
		// Do nothing, and wait for the sequence numbers to catch up.
		fmt.Println("**************** Sequence number", data.SeqSequenceNumber, "not expected. Too low. Expecting", data.ExpectedSeqSequenceNumber)
		return false
	}
	// fmt.Println("Seq session:", data.SessionId)
	// fmt.Println("Seq sequence:", data.SeqSequenceNumber)
	// fmt.Println("Seq App payloads:", data.NumberOfAppPayloads)
	return true
}

// Decode the bytes in a message from an App
func decodeAppMessage(data *AppCommData, expectedSequenceForApp *map[uint64]uint64) bool {
	data.Type = binary.BigEndian.Uint16(data.MasterBuffer[0:2])
	data.PayloadSize = binary.BigEndian.Uint16(data.MasterBuffer[2:4])
	data.Id = binary.BigEndian.Uint64(data.MasterBuffer[4:12])
	data.AppSequenceNumber = binary.BigEndian.Uint64(data.MasterBuffer[12:20])
	data.Payload = data.MasterBuffer[20 : 20+data.PayloadSize]

	/*
		Here's how the Sequencer gap handling should work:
		- At initialization, set ExpectedAppSequenceNumber to 0
		- Read the App message, and decode AppSequenceNumber
		- if ExpectedAppSequenceNumber == AppSequenceNumber then
		-   increment ExpectedAppSequenceNumber
		- else
		-   ignore App message

		App sequence number handling should have two possible scenarios:
		- expected sequence number - continue
		- lower sequence number than expected - do nothing
	*/

	if _, ok := (*expectedSequenceForApp)[data.Id]; ok {
		fmt.Println("found pre-existing entry for app id", data.Id)
	} else {
		fmt.Println("couldn't find a previous entry for app id", data.Id)
		(*expectedSequenceForApp)[data.Id] = 0
	}

	if (*expectedSequenceForApp)[data.Id] != data.AppSequenceNumber {
		// Do nothing, and wait for the sequence numbers to catch up.
		fmt.Println("**************** Sequence number", data.AppSequenceNumber, "not expected. Expecting", (*expectedSequenceForApp)[data.Id])
		return false
	}
	fmt.Println("App type:", data.Type)
	fmt.Println("App size:", data.PayloadSize)
	fmt.Println("App id:", data.Id)
	fmt.Println("App sequence number:", data.AppSequenceNumber)
	fmt.Printf("App payload: \"%s\"\n", string(data.Payload))
	(*expectedSequenceForApp)[data.Id]++
	return true

}

// Encode as bytes and send an App message to the sequencer
func sendAppMessage(data *AppCommData, connection *net.UDPConn) {
	// Clear data buffers
	data.MasterBuffer = data.MasterBuffer[:0] // Clear the byte slice send buffer

	data.PayloadSize = uint16(len(data.Payload))

	// Convert fields into byte arrays
	binary.BigEndian.PutUint16(data.TypeBuffer, data.Type)
	binary.BigEndian.PutUint16(data.SizeBuffer, data.PayloadSize)
	binary.BigEndian.PutUint64(data.IdBuffer, data.Id)
	binary.BigEndian.PutUint64(data.AppSequenceNumberBuffer, data.AppSequenceNumber)

	// Add byte arrays to master output buffer
	data.MasterBuffer = append(data.MasterBuffer, data.TypeBuffer...)
	data.MasterBuffer = append(data.MasterBuffer, data.SizeBuffer...)
	data.MasterBuffer = append(data.MasterBuffer, data.IdBuffer...)
	data.MasterBuffer = append(data.MasterBuffer, data.AppSequenceNumberBuffer...)

	// Add payload to master output buffer
	data.MasterBuffer = append(data.MasterBuffer, data.Payload...)

	connection.Write(data.MasterBuffer)
	data.AppSequenceNumber++ // Increment App sequence number every time we've sent a datagram
}

// Fetch configuration parameters from JSON file
func getConfiguration(filename string) Configuration {
	file, _ := os.Open(filename)
	defer file.Close()
	decoder := json.NewDecoder(file)
	configuration := Configuration{}
	err := decoder.Decode(&configuration)
	if err != nil {
		fmt.Println("error:", err)
	}
	return configuration
}

// Encode as bytes and send a Seq message to the apps
func sendSeqMessage(sinkData *AppCommData, riseData *SeqCommData, connection *net.UDPConn) {

	fmt.Println("riseData.NumberOfAppPayloads", riseData.NumberOfAppPayloads)
	// Clear riseData buffers
	riseData.MasterBuffer = riseData.MasterBuffer[:0] // Clear the byte slice send buffer

	// Convert fields into byte arrays
	binary.BigEndian.PutUint64(riseData.SessionIdBuffer, riseData.SessionId)
	binary.BigEndian.PutUint64(riseData.SeqSequenceNumberBuffer, riseData.SeqSequenceNumber)
	binary.BigEndian.PutUint16(riseData.NumberOfAppPayloadsBuffer, riseData.NumberOfAppPayloads)

	// Add byte arrays to master output buffer
	riseData.MasterBuffer = append(riseData.MasterBuffer, riseData.SessionIdBuffer...)
	riseData.MasterBuffer = append(riseData.MasterBuffer, riseData.SeqSequenceNumberBuffer...)
	riseData.MasterBuffer = append(riseData.MasterBuffer, riseData.NumberOfAppPayloadsBuffer...)

	// Add payload to master output buffer
	appDataSize := sinkData.PayloadSize + 20 // Size of App packet
	riseData.MasterBuffer = append(riseData.MasterBuffer, sinkData.MasterBuffer[0:appDataSize]...)
	connection.Write(riseData.MasterBuffer)
	riseData.SeqSequenceNumber++ // Increment App sequence number every time we've sent a datagram
}
