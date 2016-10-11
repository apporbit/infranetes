// Copyright 2012 Apcera Inc. All rights reserved.

// Package uuid provides functionality for generating universally unique
// identifiers. (see http://en.wikipedia.org/wiki/Universally_unique_identifier)
package uuid

import (
	"bytes"
	"crypto/md5"
	crypto_rand "crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	math_rand "math/rand"
	"net"
	"sync"
	"time"
)

// The number of nano seconds between the switch to the Gregorian Calendar and
// Unix Epoch, divided by 100. This is used in Variant1() calculations.
const Variant1EpochOffset = 12219292800000

// The length of the byte array for a UUID in memory.
const UUIDByteLen = 16

// The length of a string representation of a UUID.
const UUIDStringLen = 36

// The length of the hardware component of a Variant1 UUID.
const UUIDHardwareByteLen = 6

type UUID []byte
type UUIDSlice []byte

// Returns the length of this data, should always be 16.
func (u UUIDSlice) Len() int {
	return len(u)
}

// Compares two the other UUID and returns true if this object is "less than"
// the other object.
func (u UUIDSlice) Less(i, j int) bool {
	return u[i] < u[j]
}

// Swaps the elements at index i and j in the given Slice.
func (u UUIDSlice) Swap(i, j int) {
	u[i], u[j] = u[j], u[i]
}

// Used to ensure we only setup Variant1 data one time.
var setupOnce sync.Once

// Stores the binary node name. This is typically MAC address but may also be
// random data if the MAC can not be discovered.
var nodeName []byte

// Stores the last time a Variant1 UUID was generated in order to ensure we do
// not produce conflicts.
var lastTime int64

// Stores the clock correction value used in Variant1 generation.
var clockCorrect uint16

// Sets the "node name" value that will be used for the life of this process.
//
// This is currently not completely safe as it will fall back to using random
// data if no mac address can be found, and if two machines fall into that
// state it is possible for them to generate colliding UUIDs.
func setNodeName() {
	// Implementation specific optimization. Pre populate the counter with
	// a random seed to reduce the risk of collision.
	clockCorrect = uint16(math_rand.Int31() & 0xffff)

	// Get a mac address from an interface on this machine.
	interfaces, _ := net.Interfaces()
	if interfaces != nil {
		for _, i := range interfaces {
			if len(i.HardwareAddr) == UUIDHardwareByteLen {
				nodeName = i.HardwareAddr
				return
			}
		}
	}

	// Randomly generated node names should always have the LSB of the first
	// byte set to 1 as per the IEEE.
	nodeName = make([]byte, UUIDHardwareByteLen)
	io.ReadFull(crypto_rand.Reader, nodeName)
	nodeName[0] = nodeName[0] | 0x01
}

// Generates a "Variant 1" style UUID. This uses the machines MAC address, and
// the time since 15 October 1582 in nanoseconds, divided by 100.
//
// This form of UUID is useful when you do not care if you leak MAC info, can
// be sure that MAC addresses are not duplicated on your network, and can
// be sure that no more than 9,999 UUIDs will be generated every 100ns.
func Variant1() (u UUID) {
	// Format is as follows
	// Time stamp (60 bits): aaabbbbcccccccc
	// clock id (16 bits): dddd
	// node id (48 bits): eeeeeeeeeeee
	// Output is: cccccccc-bbbb-1aaa-dddd-Eeeeeeeeeeee
	// where 1 is mandated, and E must have its MSB set.

	setupOnce.Do(setNodeName)

	// UUID uses time as nano seconds since the west adopted the
	// Gregorian calendar, divided by 100. We manage this by adding
	// a precomputed offset since Unix() uses time since Jan 1, 1970.
	ts := (time.Now().UnixNano() / 100) + Variant1EpochOffset

	// Clock correct gets incremented every time we generate two UUIDs in the
	// same 100 nano second period. Should be rare.
	if ts == lastTime {
		clockCorrect += 1
	} else {
		lastTime = ts
	}

	u = make([]byte, UUIDByteLen)
	u[0] = byte((ts >> (3 * 8)) & 0xff)
	u[1] = byte((ts >> (2 * 8)) & 0xff)
	u[2] = byte((ts >> (1 * 8)) & 0xff)
	u[3] = byte(ts & 0xff)
	u[4] = byte((ts >> (5 * 8)) & 0xff)
	u[5] = byte((ts >> (4 * 8)) & 0xff)
	u[6] = byte((ts>>(7*8))&0x0f + 0x10)
	u[7] = byte((ts >> (6 * 8)) & 0xff)
	u[8] = byte((clockCorrect>>1)&0x1f) | 0x80
	u[9] = byte(clockCorrect & 0xff)
	u[10] = byte(nodeName[0])
	u[11] = byte(nodeName[1])
	u[12] = byte(nodeName[2])
	u[13] = byte(nodeName[3])
	u[14] = byte(nodeName[4])
	u[15] = byte(nodeName[5])

	return u
}

// RFC4122 defined DNS name space UUID for Variant 3 and 5 UUIDs.
func NameSpaceDNS() UUID {
	return UUID{107, 167, 184, 16, 157, 173, 17, 209,
		128, 180, 0, 192, 79, 212, 48, 200}
}

// RFC4122 defined URL name space UUID for Variant 3 and 5 UUIDs.
func NameSpaceURL() UUID {
	return UUID{107, 167, 184, 17, 157, 173, 17, 209,
		128, 180, 0, 192, 79, 212, 48, 200}
}

// RFC4122 defined OID name space UUID for Variant 3 and 5 UUIDs.
func NameSpaceOID() UUID {
	return UUID{107, 167, 184, 18, 157, 173, 17, 209,
		128, 180, 0, 192, 79, 212, 48, 200}
}

// RFC4122 defined X500 name space UUID for Variant 3 and 5 UUIDs.
func NameSpaceX500() UUID {
	return UUID{107, 167, 184, 18, 157, 173, 17, 209,
		128, 180, 0, 192, 79, 212, 48, 200}
}

// Generate a "Variant 3" style UUID. These UUIDs are not time based, instead
// using a namespace and a string to generate a hashed number. Variant 3
// is MD5 based and should only be used for legacy reasons. Use Variant 5 for
// all new projects.
func Variant3(namespace UUID, name string) (u UUID) {
	h := md5.New()
	h.Write([]byte(namespace))
	h.Write([]byte(name))
	u = h.Sum(nil)[0:16]
	u[6] = (u[6] & 0x0f) | 0x30
	u[8] = (u[8] & 0x1f) | 0x80
	return u
}

// Generates a "Variant 4" style UUID. These are nothing more than random
// data with the proper reserved bits set.
//
// A random UUID has not assurance that it is in fact unique. These are only
// usable if you can survive duplication, or have a central source to
// verify uniqueness.
func Variant4() (u UUID) {
	// Output is: rrrrrrrr-rrrr-4rrr-Rrrr-rrrrrrrrrrrr
	// where 4 is mandated, and R must be one of 8, 9, A or B.
	u = make([]byte, UUIDByteLen)
	io.ReadFull(crypto_rand.Reader, u)
	u[6] = (u[6] & 0x0f) + 0x40
	u[8] = (u[8] & 0x1f) + 0x80

	return u
}

// Generate a "Variant 5" style UUID. These are functionally the same as
// Variant 3, using sha1 rather than md5. Variant 5 UUIDs are recommended for
// all new designs where Variant 3 would have normally been used. It takes a
// given UUID as a name space and a string identifier, given the same name
// space and string this will produce the same UUID.
func Variant5(namespace UUID, name string) (u UUID) {
	h := sha1.New()
	h.Write([]byte(namespace))
	h.Write([]byte(name))
	u = h.Sum(nil)[0:16]
	u[6] = (u[6] & 0x0f) | 0x50
	u[8] = (u[8] & 0x3f) | 0x80
	return u
}

// Generates a Variant 1 UUID. This may change in the future so the semantics
// should be assumed that this returns a vaguely unique 128bit blob.
func Generate() UUID {
	return Variant1()
}

// Compares two UUID objects.
func (u UUID) Compare(o UUID) int {
	return bytes.Compare(u, o)
}

// Checks to see if two UUID objects are equal.
func (u UUID) Equal(o UUID) bool {
	return bytes.Equal(u, o)
}

// Used for internal string generation.
var hexBytes = [...]byte{
	'0', '1', '2', '3', '4', '5', '6', '7',
	'8', '9', 'a', 'b', 'c', 'd', 'e', 'f',
}

// Returns a string representation of the given UUID data.
func (u UUID) String() string {
	output := make([]byte, UUIDStringLen)

	output[0] = hexBytes[(u[0]&0xf0)>>4]
	output[1] = hexBytes[(u[0] & 0x0f)]
	output[2] = hexBytes[(u[1]&0xf0)>>4]
	output[3] = hexBytes[(u[1] & 0x0f)]
	output[4] = hexBytes[(u[2]&0xf0)>>4]
	output[5] = hexBytes[(u[2] & 0x0f)]
	output[6] = hexBytes[(u[3]&0xf0)>>4]
	output[7] = hexBytes[(u[3] & 0x0f)]
	output[8] = '-'
	output[9] = hexBytes[(u[4]&0xf0)>>4]
	output[10] = hexBytes[(u[4] & 0x0f)]
	output[11] = hexBytes[(u[5]&0xf0)>>4]
	output[12] = hexBytes[(u[5] & 0x0f)]
	output[13] = '-'
	output[14] = hexBytes[(u[6]&0xf0)>>4]
	output[15] = hexBytes[(u[6] & 0x0f)]
	output[16] = hexBytes[(u[7]&0xf0)>>4]
	output[17] = hexBytes[(u[7] & 0x0f)]
	output[18] = '-'
	output[19] = hexBytes[(u[8]&0xf0)>>4]
	output[20] = hexBytes[(u[8] & 0x0f)]
	output[21] = hexBytes[(u[9]&0xf0)>>4]
	output[22] = hexBytes[(u[9] & 0x0f)]
	output[23] = '-'
	output[24] = hexBytes[(u[10]&0xf0)>>4]
	output[25] = hexBytes[(u[10] & 0x0f)]
	output[26] = hexBytes[(u[11]&0xf0)>>4]
	output[27] = hexBytes[(u[11] & 0x0f)]
	output[28] = hexBytes[(u[12]&0xf0)>>4]
	output[29] = hexBytes[(u[12] & 0x0f)]
	output[30] = hexBytes[(u[13]&0xf0)>>4]
	output[31] = hexBytes[(u[13] & 0x0f)]
	output[32] = hexBytes[(u[14]&0xf0)>>4]
	output[33] = hexBytes[(u[14] & 0x0f)]
	output[34] = hexBytes[(u[15]&0xf0)>>4]
	output[35] = hexBytes[(u[15] & 0x0f)]

	return string(output)
}

// Returns the string representation, quoted, as bytes, for JSON encoding
func (u UUID) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%s\"", u.String())), nil
}

// Returns the UUID as a series of bytes in an array.
func (u UUID) Bytes() []byte {
	return u
}

// Error thrown when a UUID can not be parsed from a string.
type BadUUIDStringError struct {
	message string
}

func (e *BadUUIDStringError) Error() string {
	return e.message
}

// Converts a string representation of a UUID into a UUID object.
func FromString(s string) (u UUID, e error) {
	if len(s) != UUIDStringLen {
		return nil, &BadUUIDStringError{"length is not 36 bytes: " + s}
	}

	// Make enough space for the storage.
	u = make([]byte, UUIDByteLen)
	s_byte := []byte(s)

	var i int
	var err error

	i, err = hex.Decode(u[0:4], s_byte[0:8])
	if err != nil || i != 4 {
		return nil, &BadUUIDStringError{"Invalid first component: " + s}
	}

	if s_byte[8] != '-' {
		return nil, &BadUUIDStringError{"Position 8 is not a dash: " + s}
	}

	i, err = hex.Decode(u[4:6], s_byte[9:13])
	if err != nil || i != 2 {
		return nil, &BadUUIDStringError{"Invalid second component: " + s}
	}

	if s_byte[13] != '-' {
		return nil, &BadUUIDStringError{"Position 13 is not a dash: " + s}
	}

	i, err = hex.Decode(u[6:8], s_byte[14:18])
	if err != nil || i != 2 {
		return nil, &BadUUIDStringError{"Invalid third component: " + s}
	}

	if s_byte[18] != '-' {
		return nil, &BadUUIDStringError{"Position 18 is not a dash: " + s}
	}

	i, err = hex.Decode(u[8:10], s_byte[19:23])
	if err != nil || i != 2 {
		return nil, &BadUUIDStringError{"Invalid fourth component: " + s}
	}

	if s_byte[23] != '-' {
		return nil, &BadUUIDStringError{"Position 23 is not a dash: " + s}
	}

	i, err = hex.Decode(u[10:16], s_byte[24:36])
	if err != nil || i != 6 {
		return nil, &BadUUIDStringError{"Invalid fifth component: " + s}
	}

	if u[8]&0xc0 != 0x80 {
		return nil, &BadUUIDStringError{"Reserved bits used: " + s}
	}

	return u, nil
}

// Error thrown when a UUID can not be parsed from a string.
type NotAUUIDError struct {
	message      string
	WantedLength int
}

func (e *NotAUUIDError) Error() string {
	return e.message
}

// Constructs a UUID from a raw bytes object
func FromBytes(raw []byte) (u UUID, e error) {
	if len(raw) != UUIDByteLen {
		return nil, &NotAUUIDError{
			fmt.Sprintf(
				"Length of memory object should be %d, is %d",
				UUIDByteLen, len(raw)), len(raw)}
	}
	if u[8]&0xc0 != 0x80 {
		return nil, &BadUUIDStringError{"Reserved bits used"}
	}
	return UUID(raw), nil
}
