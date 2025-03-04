package crypto

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"regexp"
	"runtime"

	"github.com/ProtonMail/gopenpgp/armor"
	"github.com/ProtonMail/gopenpgp/constants"
	"github.com/ProtonMail/gopenpgp/internal"

	"golang.org/x/crypto/openpgp/clearsign"
	"golang.org/x/crypto/openpgp/packet"
)

// ---- MODELS -----

// PlainMessage stores an unencrypted message.
type PlainMessage struct {
	// The content of the message
	Data []byte
	// if the content is text or binary
	TextType bool
}

// PGPMessage stores a PGP-encrypted message.
type PGPMessage struct {
	// The content of the message
	Data []byte
}

// PGPSignature stores a PGP-encoded detached signature.
type PGPSignature struct {
	// The content of the signature
	Data []byte
}

// PGPSplitMessage contains a separate session key packet and symmetrically
// encrypted data packet.
type PGPSplitMessage struct {
	DataPacket []byte
	KeyPacket  []byte
}

// ClearTextMessage, split signed clear text message container
type ClearTextMessage struct {
	Data []byte
	Signature []byte
}

// ---- GENERATORS -----

// NewPlainMessage generates a new binary PlainMessage ready for encryption,
// signature, or verification from the unencrypted binary data.
func NewPlainMessage(data []byte) *PlainMessage {
	return &PlainMessage{
		Data:     data,
		TextType: false,
	}
}

// NewPlainMessageFromString generates a new text PlainMessage,
// ready for encryption, signature, or verification from an unencrypted string.
func NewPlainMessageFromString(text string) *PlainMessage {
	return &PlainMessage{
		Data:     []byte(text),
		TextType: true,
	}
}

// NewPGPMessage generates a new PGPMessage from the unarmored binary data.
func NewPGPMessage(data []byte) *PGPMessage {
	return &PGPMessage{
		Data: data,
	}
}

// NewPGPMessageFromArmored generates a new PGPMessage from an armored string ready for decryption.
func NewPGPMessageFromArmored(armored string) (*PGPMessage, error) {
	encryptedIO, err := internal.Unarmor(armored)
	if err != nil {
		return nil, err
	}

	message, err := ioutil.ReadAll(encryptedIO.Body)
	if err != nil {
		return nil, err
	}

	return &PGPMessage{
		Data: message,
	}, nil
}

// NewPGPSplitMessage generates a new PGPSplitMessage from the binary unarmored keypacket,
// datapacket, and encryption algorithm.
func NewPGPSplitMessage(keyPacket []byte, dataPacket []byte) *PGPSplitMessage {
	return &PGPSplitMessage{
		KeyPacket:  keyPacket,
		DataPacket: dataPacket,
	}
}

// NewPGPSplitMessageFromArmored generates a new PGPSplitMessage by splitting an armored message into its
// session key packet and symmetrically encrypted data packet.
func NewPGPSplitMessageFromArmored(encrypted string) (*PGPSplitMessage, error) {
	message, err := NewPGPMessageFromArmored(encrypted)
	if err != nil {
		return nil, err
	}

	return message.SeparateKeyAndData(len(encrypted), -1)
}

// NewPGPSignature generates a new PGPSignature from the unarmored binary data.
func NewPGPSignature(data []byte) *PGPSignature {
	return &PGPSignature{
		Data: data,
	}
}

// NewPGPSignatureFromArmored generates a new PGPSignature from the armored string ready for verification.
func NewPGPSignatureFromArmored(armored string) (*PGPSignature, error) {
	encryptedIO, err := internal.Unarmor(armored)
	if err != nil {
		return nil, err
	}

	signature, err := ioutil.ReadAll(encryptedIO.Body)
	if err != nil {
		return nil, err
	}

	return &PGPSignature{
		Data: signature,
	}, nil
}

// NewClearTextMessage generates a new ClearTextMessage from data and signature
func NewClearTextMessage(data []byte, signature []byte) *ClearTextMessage {
	return &ClearTextMessage{
		Data:  data,
		Signature: signature,
	}
}

// NewClearTextMessageFromArmored returns the message body and unarmored signature from a clearsigned message.
func NewClearTextMessageFromArmored(signedMessage string) (*ClearTextMessage, error) {
	modulusBlock, rest := clearsign.Decode([]byte(signedMessage))
	if len(rest) != 0 {
		return nil, errors.New("pmapi: extra data after modulus")
	}

	signature, err := ioutil.ReadAll(modulusBlock.ArmoredSignature.Body)
	if err != nil {
		return nil, err
	}

	return NewClearTextMessage(modulusBlock.Bytes, signature), nil
}

// ---- MODEL METHODS -----

// GetBinary returns the binary content of the message as a []byte
func (msg *PlainMessage) GetBinary() []byte {
	return msg.Data
}

// GetString returns the content of the message as a string
func (msg *PlainMessage) GetString() string {
	return string(msg.Data)
}

// GetBase64 returns the base-64 encoded binary content of the message as a string
func (msg *PlainMessage) GetBase64() string {
	return base64.StdEncoding.EncodeToString(msg.Data)
}

// NewReader returns a New io.Reader for the bianry data of the message
func (msg *PlainMessage) NewReader() io.Reader {
	return bytes.NewReader(msg.GetBinary())
}

// IsText returns whether the message is a text message
func (msg *PlainMessage) IsText() bool {
	return msg.TextType
}

// IsBinary returns whether the message is a binary message
func (msg *PlainMessage) IsBinary() bool {
	return !msg.TextType
}

// GetBinary returns the unarmored binary content of the message as a []byte
func (msg *PGPMessage) GetBinary() []byte {
	return msg.Data
}

// NewReader returns a New io.Reader for the unarmored bianry data of the message
func (msg *PGPMessage) NewReader() io.Reader {
	return bytes.NewReader(msg.GetBinary())
}

// GetArmored returns the armored message as a string
func (msg *PGPMessage) GetArmored() (string, error) {
	return armor.ArmorWithType(msg.Data, constants.PGPMessageHeader)
}

// GetBinaryDataPacket returns the unarmored binary datapacket as a []byte
func (msg *PGPSplitMessage) GetBinaryDataPacket() []byte {
	return msg.DataPacket
}

// GetBinaryKeyPacket returns the unarmored binary keypacket as a []byte
func (msg *PGPSplitMessage) GetBinaryKeyPacket() []byte {
	return msg.KeyPacket
}

// SeparateKeyAndData returns the first keypacket and the (hopefully unique) dataPacket (not verified)
func (msg *PGPMessage) SeparateKeyAndData(estimatedLength, garbageCollector int) (outSplit *PGPSplitMessage, err error) {
	// For info on each, see: https://golang.org/pkg/runtime/#MemStats
	packets := packet.NewReader(bytes.NewReader(msg.Data))
	outSplit = &PGPSplitMessage{}
	gcCounter := 0

	// Store encrypted key and symmetrically encrypted packet separately
	var encryptedKey *packet.EncryptedKey
	var decryptErr error
	for {
		var p packet.Packet
		if p, err = packets.Next(); err == io.EOF {
			err = nil
			break
		}
		switch p := p.(type) {
		case *packet.EncryptedKey:
			if encryptedKey != nil && encryptedKey.Key != nil {
				break
			}
			encryptedKey = p

		case *packet.SymmetricallyEncrypted:
			// FIXME: add support for multiple keypackets
			var b bytes.Buffer
			// 2^16 is an estimation of the size difference between input and output, the size difference is most probably
			// 16 bytes at a maximum though.
			// We need to avoid triggering a grow from the system as this will allocate too much memory causing problems
			// in low-memory environments
			b.Grow(1<<16 + estimatedLength)
			// empty encoded length + start byte
			b.Write(make([]byte, 6))
			b.WriteByte(byte(1))
			actualLength := 1
			block := make([]byte, 128)
			for {
				n, err := p.Contents.Read(block)
				if err == io.EOF {
					break
				}
				b.Write(block[:n])
				actualLength += n
				gcCounter += n
				if gcCounter > garbageCollector && garbageCollector > 0 {
					runtime.GC()
					gcCounter = 0
				}
			}

			// quick encoding
			symEncryptedData := b.Bytes()
			if actualLength < 192 {
				symEncryptedData[4] = byte(210)
				symEncryptedData[5] = byte(actualLength)
				symEncryptedData = symEncryptedData[4:]
			} else if actualLength < 8384 {
				actualLength = actualLength - 192
				symEncryptedData[3] = byte(210)
				symEncryptedData[4] = 192 + byte(actualLength>>8)
				symEncryptedData[5] = byte(actualLength)
				symEncryptedData = symEncryptedData[3:]
			} else {
				symEncryptedData[0] = byte(210)
				symEncryptedData[1] = byte(255)
				symEncryptedData[2] = byte(actualLength >> 24)
				symEncryptedData[3] = byte(actualLength >> 16)
				symEncryptedData[4] = byte(actualLength >> 8)
				symEncryptedData[5] = byte(actualLength)
			}

			outSplit.DataPacket = symEncryptedData
		}
	}
	if decryptErr != nil {
		return nil, fmt.Errorf("gopenpgp: cannot decrypt encrypted key packet: %v", decryptErr)
	}
	if encryptedKey == nil {
		return nil, errors.New("gopenpgp: packets don't include an encrypted key packet")
	}

	var buf bytes.Buffer
	if err := encryptedKey.Serialize(&buf); err != nil {
		return nil, fmt.Errorf("gopenpgp: cannot serialize encrypted key: %v", err)
	}
	outSplit.KeyPacket = buf.Bytes()

	return outSplit, nil
}

// GetBinary returns the unarmored binary content of the signature as a []byte
func (msg *PGPSignature) GetBinary() []byte {
	return msg.Data
}

// GetArmored returns the armored signature as a string
func (msg *PGPSignature) GetArmored() (string, error) {
	return armor.ArmorWithType(msg.Data, constants.PGPSignatureHeader)
}

// GetBinary returns the unarmored signed data as a []byte
func (msg *ClearTextMessage) GetBinary() []byte {
	return msg.Data
}

// GetString returns the unarmored signed data as a string
func (msg *ClearTextMessage) GetString() string {
	return string(msg.Data)
}

// GetBinarySignature returns the unarmored binary signature as a []byte
func (msg *ClearTextMessage) GetBinarySignature() []byte {
	return msg.Signature
}

// GetArmored armors plaintext and signature with the PGP SIGNED MESSAGE armoring
func (msg *ClearTextMessage) GetArmored() (string, error) {
	armSignature, err := armor.ArmorWithType(msg.GetBinarySignature(), constants.PGPSignatureHeader)
	if err != nil {
		return "", err
	}

	str := "-----BEGIN PGP SIGNED MESSAGE-----\r\nHash:SHA512\r\n\r\n"
	str += msg.GetString()
	str += "\r\n"
	str += armSignature

	return str, nil
}

// ---- UTILS -----

// IsPGPMessage checks if data if has armored PGP message format.
func (pgp *GopenPGP) IsPGPMessage(data string) bool {
	re := regexp.MustCompile("^-----BEGIN " + constants.PGPMessageHeader + "-----(?s:.+)-----END " +
		constants.PGPMessageHeader + "-----")
	return re.MatchString(data)
}
