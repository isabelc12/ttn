// Copyright © 2016 The Things Network
// Use of this source code is governed by the MIT license that can be found in the LICENSE file.

package broker

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"sync"

	dbutil "github.com/TheThingsNetwork/ttn/core/storage"
	"github.com/TheThingsNetwork/ttn/utils/errors"
)

// NetworkController gives a facade for manipulating the broker databases and devices
type NetworkController interface {
	LookupDevices(devAddr []byte) ([]devEntry, error)
	WholeCounter(devCnt uint32, entryCnt uint32) (uint32, error)
	StoreDevice(devAddr []byte, entry devEntry) error
	UpdateFCnt(appEUI []byte, devEUI []byte, devAddr []byte, fcnt uint32) error
	Close() error
}

type devEntry struct {
	Dialer  Dialer
	AppEUI  []byte
	DevEUI  []byte
	NwkSKey [16]byte
	FCntUp  uint32
}

type controller struct {
	sync.RWMutex
	db           dbutil.Interface
	Devices      string
	Applications string
}

// NewNetworkController constructs a new broker controller
func NewNetworkController(name string) (NetworkController, error) {
	itf, err := dbutil.New(name)
	if err != nil {
		return nil, errors.New(errors.Operational, err)
	}

	return &controller{db: itf, Devices: "Devices", Applications: "Applications"}, nil
}

// LookupDevices implements the broker.NetworkController interface
func (s *controller) LookupDevices(devAddr []byte) ([]devEntry, error) {
	s.RLock()
	defer s.RUnlock()
	entries, err := s.db.Lookup(s.Devices, devAddr, &devEntry{})
	if err != nil {
		return nil, err
	}
	return entries.([]devEntry), nil
}

// WholeCounter implements the broker.NetworkController interface
func (s *controller) WholeCounter(devCnt uint32, entryCnt uint32) (uint32, error) {
	upperSup := int(math.Pow(2, 16))
	diff := int(devCnt) - (int(entryCnt) % upperSup)
	var offset int
	if diff >= 0 {
		offset = diff
	} else {
		offset = upperSup + diff
	}
	if offset > upperSup/4 {
		return 0, errors.New(errors.Structural, "Gap too big, counter is errored")
	}
	return entryCnt + uint32(offset), nil
}

// UpdateFCnt implements the broker.NetworkController interface
func (s *controller) UpdateFCnt(appEUI []byte, devEUI []byte, devAddr []byte, fcnt uint32) error {
	s.Lock()
	defer s.Unlock()
	itf, err := s.db.Lookup(s.Devices, devAddr, &devEntry{})
	if err != nil {
		return err
	}
	entries := itf.([]devEntry)

	var newEntries []dbutil.Entry
	for _, e := range entries {
		entry := new(devEntry)
		*entry = e
		if reflect.DeepEqual(entry.AppEUI, appEUI) && reflect.DeepEqual(entry.DevEUI, devEUI) {
			entry.FCntUp = fcnt
		}
		newEntries = append(newEntries, entry)
	}

	return s.db.Replace(s.Devices, devAddr, newEntries)
}

// StoreDevice implements the broker.NetworkController interface
func (s *controller) StoreDevice(devAddr []byte, entry devEntry) error {
	s.Lock()
	defer s.Unlock()
	return s.db.Store(s.Devices, devAddr, []dbutil.Entry{&entry})
}

// Close implements the broker.NetworkController interface
func (s *controller) Close() error {
	return s.db.Close()
}

// MarshalBinary implements the encoding.BinaryMarshaler interface
func (e devEntry) MarshalBinary() ([]byte, error) {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, e.AppEUI)  // 8
	binary.Write(buf, binary.BigEndian, e.DevEUI)  // 8
	binary.Write(buf, binary.BigEndian, e.NwkSKey) // 16
	binary.Write(buf, binary.BigEndian, e.FCntUp)  // 4
	if len(buf.Bytes()) != 36 {
		return nil, errors.New(errors.Structural, "Device entry was invalid. Cannot Marshal")
	}
	binary.Write(buf, binary.BigEndian, e.Dialer.MarshalSafely())
	return buf.Bytes(), nil
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface
func (e *devEntry) UnmarshalBinary(data []byte) error {
	buf := bytes.NewBuffer(data)
	e.AppEUI = make([]byte, 8, 8)
	binary.Read(buf, binary.BigEndian, &e.AppEUI)
	e.DevEUI = make([]byte, 8, 8)
	binary.Read(buf, binary.BigEndian, &e.DevEUI)
	binary.Read(buf, binary.BigEndian, &e.NwkSKey) // fixed-length array
	if err := binary.Read(buf, binary.BigEndian, &e.FCntUp); err != nil {
		return errors.New(errors.Structural, err)
	}
	e.Dialer = NewDialer(buf.Next(buf.Len()))
	return nil
}