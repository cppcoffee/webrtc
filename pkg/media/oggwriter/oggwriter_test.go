// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package oggwriter

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/pion/rtp"
	"github.com/stretchr/testify/assert"
)

type oggWriterPacketTest struct {
	buffer       io.Writer
	message      string
	messageClose string
	packet       *rtp.Packet
	writer       *OggWriter
	err          error
	closeErr     error
}

func TestOggWriter_AddPacketAndClose(t *testing.T) {
	rawPkt := []byte{
		0x90, 0xe0, 0x69, 0x8f, 0xd9, 0xc2, 0x93, 0xda, 0x1c, 0x64,
		0x27, 0x82, 0x00, 0x01, 0x00, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0x98, 0x36, 0xbe, 0x88, 0x9e,
	}

	validPacket := &rtp.Packet{
		Header: rtp.Header{
			Marker:           true,
			Extension:        true,
			ExtensionProfile: 1,
			Version:          2,
			PayloadType:      111,
			SequenceNumber:   27023,
			Timestamp:        3653407706,
			SSRC:             476325762,
			CSRC:             []uint32{},
		},
		Payload: rawPkt[20:],
	}
	assert.NoError(t, validPacket.SetExtension(0, []byte{0xFF, 0xFF, 0xFF, 0xFF}))

	assert := assert.New(t)

	// The linter misbehave and thinks this code is the same as the tests in ivf-writer_test
	// nolint:dupl
	addPacketTestCase := []oggWriterPacketTest{
		{
			buffer:       &bytes.Buffer{},
			message:      "OggWriter shouldn't be able to write something to a closed file",
			messageClose: "OggWriter should be able to close an already closed file",
			packet:       validPacket,
			err:          errFileNotOpened,
			closeErr:     nil,
		},
		{
			buffer:       &bytes.Buffer{},
			message:      "OggWriter shouldn't be able to write a nil packet",
			messageClose: "OggWriter should be able to close the file",
			packet:       nil,
			err:          errInvalidNilPacket,
			closeErr:     nil,
		},
		{
			buffer:       &bytes.Buffer{},
			message:      "OggWriter should be able to write an Opus packet",
			messageClose: "OggWriter should be able to close the file",
			packet:       validPacket,
			err:          nil,
			closeErr:     nil,
		},
		{
			buffer:       nil,
			message:      "OggWriter shouldn't be able to write something to a closed file",
			messageClose: "OggWriter should be able to close an already closed file",
			packet:       nil,
			err:          errFileNotOpened,
			closeErr:     nil,
		},
	}

	// First test case has a 'nil' file descriptor
	writer, err := NewWith(addPacketTestCase[0].buffer, 48000, 2)
	assert.Nil(err, "OggWriter should be created")
	assert.NotNil(writer, "Writer shouldn't be nil")
	err = writer.Close()
	assert.Nil(err, "OggWriter should be able to close the file descriptor")
	writer.stream = nil
	addPacketTestCase[0].writer = writer

	// Second test writes tries to write an empty packet
	writer, err = NewWith(addPacketTestCase[1].buffer, 48000, 2)
	assert.Nil(err, "OggWriter should be created")
	assert.NotNil(writer, "Writer shouldn't be nil")
	addPacketTestCase[1].writer = writer

	// Third test writes tries to write a valid Opus packet
	writer, err = NewWith(addPacketTestCase[2].buffer, 48000, 2)
	assert.Nil(err, "OggWriter should be created")
	assert.NotNil(writer, "Writer shouldn't be nil")
	addPacketTestCase[2].writer = writer

	// Fourth test tries to write to a nil stream
	writer, err = NewWith(addPacketTestCase[3].buffer, 4800, 2)
	assert.NotNil(err, "IVFWriter shouldn't be created")
	assert.Nil(writer, "Writer should be nil")
	addPacketTestCase[3].writer = writer

	for _, t := range addPacketTestCase {
		if t.writer != nil {
			res := t.writer.WriteRTP(t.packet)
			assert.Equal(t.err, res, t.message)
		}
	}

	for _, t := range addPacketTestCase {
		if t.writer != nil {
			res := t.writer.Close()
			assert.Equal(t.closeErr, res, t.messageClose)
		}
	}
}

func TestOggWriter_EmptyPayload(t *testing.T) {
	buffer := &bytes.Buffer{}

	writer, err := NewWith(buffer, 48000, 2)
	assert.NoError(t, err)

	assert.NoError(t, writer.WriteRTP(&rtp.Packet{Payload: []byte{}}))
}

func TestOggWriter_LargePayload(t *testing.T) {
	rawPkt := bytes.Repeat([]byte{0x45}, 1000)

	validPacket := &rtp.Packet{
		Header: rtp.Header{
			Marker:           true,
			Extension:        true,
			ExtensionProfile: 1,
			Version:          2,
			PayloadType:      111,
			SequenceNumber:   27023,
			Timestamp:        3653407706,
			SSRC:             476325762,
			CSRC:             []uint32{},
		},
		Payload: rawPkt,
	}
	assert.NoError(t, validPacket.SetExtension(0, []byte{0xFF, 0xFF, 0xFF, 0xFF}))

	writer, err := NewWith(&bytes.Buffer{}, 48000, 2)
	assert.NoError(t, err, "OggWriter should be created")
	assert.NotNil(t, writer, "Writer shouldn't be nil")

	err = writer.WriteRTP(validPacket)
	assert.NoError(t, err)

	data := writer.createPage(rawPkt, pageHeaderTypeNone, 0, 1)
	assert.Equal(t, uint8(4), data[26])
}

func TestOggWriter_VeryLargePayload(t *testing.T) {
	// Create a payload larger than 65025 bytes (255 * 255)
	// Let's use 66000 bytes.
	// 66000 / 255 = 258 remainder 210.
	// Page 1: 255 segments, 255 bytes each. Total 65025 bytes.
	// Remaining: 66000 - 65025 = 975 bytes.
	// Page 2: 975 / 255 = 3 remainder 210.
	// Segments: 255, 255, 255, 210. Total 4 segments.

	rawPkt := bytes.Repeat([]byte{0x45}, 66000)

	validPacket := &rtp.Packet{
		Header: rtp.Header{
			Marker:           true,
			Extension:        true,
			ExtensionProfile: 1,
			Version:          2,
			PayloadType:      111,
			SequenceNumber:   27023,
			Timestamp:        3653407706,
			SSRC:             476325762,
			CSRC:             []uint32{},
		},
		Payload: rawPkt,
	}

	buffer := &bytes.Buffer{}
	writer, err := NewWith(buffer, 48000, 2)
	assert.NoError(t, err)
	assert.NotNil(t, writer)

	err = writer.WriteRTP(validPacket)
	assert.NoError(t, err)

	data := buffer.Bytes()

	// Skip ID and Comment headers
	// ID Header: 19 payload + 27 header + 1 segment = 47 bytes.
	// Comment Header: 21 payload + 27 header + 1 segment = 49 bytes.
	offset := 47 + 49

	// Page 1 (Start of packet)
	if offset >= len(data) {
		t.Fatal("Page 1 missing")
	}
	// Check header type
	// offset + 5 is header type.
	// Should be 0 (pageHeaderTypeNone)
	if data[offset+5] != 0 {
		t.Errorf("Page 1 header type expected 0, got %d", data[offset+5])
	}
	// Check nSegments
	nSegments := int(data[offset+26])
	if nSegments != 255 {
		t.Errorf("Page 1 segments expected 255, got %d", nSegments)
	}
	// Check granulePos (offset 6, 8 bytes). Should be -1.
	granulePos := binary.LittleEndian.Uint64(data[offset+6 : offset+14])
	if granulePos != 0xFFFFFFFFFFFFFFFF {
		t.Errorf("Page 1 granulePos expected -1, got %d", granulePos)
	}

	// Calculate page length
	pageHeaderLen := 27 + nSegments
	payloadLen := 0
	for i := 0; i < nSegments; i++ {
		payloadLen += int(data[offset+27+i])
	}
	if payloadLen != 65025 {
		t.Errorf("Page 1 payload length expected 65025, got %d", payloadLen)
	}
	offset += pageHeaderLen + payloadLen

	// Page 2 (Continuation)
	if offset >= len(data) {
		t.Fatal("Page 2 missing")
	}
	// Check header type
	// Should be 1 (pageHeaderTypeContinuation)
	if data[offset+5] != 1 {
		t.Errorf("Page 2 header type expected 1, got %d", data[offset+5])
	}
	// Check nSegments
	nSegments = int(data[offset+26])
	// 975 / 255 = 3 remainder 210. So 4 segments.
	if nSegments != 4 {
		t.Errorf("Page 2 segments expected 4, got %d", nSegments)
	}
	// Check granulePos. Should be valid packet timestamp + increment?
	// previousGranulePosition started at 1.
	// Timestamp 3653407706.
	// It's the first packet, so increment = Timestamp - previousTimestamp (1).
	// GranulePos = 1 + (3653407706 - 1) = 3653407706.
	// But for the first packet, previousTimestamp is 1, so the condition `previousTimestamp != 1` is false.
	// So it doesn't increment. It stays 1.
	granulePos = binary.LittleEndian.Uint64(data[offset+6 : offset+14])
	if granulePos != 1 {
		t.Errorf("Page 2 granulePos expected 1, got %d", granulePos)
	}
}
