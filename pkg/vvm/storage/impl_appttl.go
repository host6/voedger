/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */
package storage

import (
	"encoding/binary"
	"unicode/utf8"

	"github.com/voedger/voedger/pkg/istructs"
)

type implAppTTLStorage struct {
	sysVVMStorage ISysVvmStorage
	clusterAppID  istructs.ClusterAppID
}

func (s *implAppTTLStorage) TTLGet(key string) (value string, ok bool, err error) {
	if err := s.validateKey(key); err != nil {
		return "", false, err
	}
	pKey, cCols := s.buildKeys(key)
	var data []byte
	ok, err = s.sysVVMStorage.Get(pKey, cCols, &data)
	if err != nil || !ok {
		return "", ok, err
	}
	return string(data), true, nil
}

func (s *implAppTTLStorage) InsertIfNotExists(key, value string, ttlSeconds int) (ok bool, err error) {
	if err := s.validateKey(key); err != nil {
		return false, err
	}
	if err := s.validateValue(value); err != nil {
		return false, err
	}
	if err := s.validateTTL(ttlSeconds); err != nil {
		return false, err
	}
	pKey, cCols := s.buildKeys(key)
	return s.sysVVMStorage.InsertIfNotExists(pKey, cCols, []byte(value), ttlSeconds)
}

func (s *implAppTTLStorage) CompareAndSwap(key, expectedValue, newValue string, ttlSeconds int) (ok bool, err error) {
	if err := s.validateKey(key); err != nil {
		return false, err
	}
	if err := s.validateValue(newValue); err != nil {
		return false, err
	}
	if err := s.validateTTL(ttlSeconds); err != nil {
		return false, err
	}
	pKey, cCols := s.buildKeys(key)
	return s.sysVVMStorage.CompareAndSwap(pKey, cCols, []byte(expectedValue), []byte(newValue), ttlSeconds)
}

func (s *implAppTTLStorage) CompareAndDelete(key, expectedValue string) (ok bool, err error) {
	if err := s.validateKey(key); err != nil {
		return false, err
	}
	pKey, cCols := s.buildKeys(key)
	return s.sysVVMStorage.CompareAndDelete(pKey, cCols, []byte(expectedValue))
}

func (s *implAppTTLStorage) buildKeys(key string) (pKey, cCols []byte) {
	pKey = make([]byte, 8)
	binary.BigEndian.PutUint32(pKey[0:4], uint32(pKeyPrefix_AppTTL))
	binary.BigEndian.PutUint32(pKey[4:8], uint32(s.clusterAppID))
	cCols = []byte(key)
	return pKey, cCols
}

func (s *implAppTTLStorage) validateKey(key string) error {
	if key == "" {
		return ErrKeyEmpty
	}
	if len(key) > MaxKeyLength {
		return ErrKeyTooLong
	}
	if !utf8.ValidString(key) {
		return ErrKeyTooLong
	}
	return nil
}

func (s *implAppTTLStorage) validateValue(value string) error {
	if len(value) > MaxValueLength {
		return ErrValueTooLong
	}
	return nil
}

func (s *implAppTTLStorage) validateTTL(ttlSeconds int) error {
	if ttlSeconds <= 0 || ttlSeconds > MaxTTLSeconds {
		return ErrInvalidTTL
	}
	return nil
}
