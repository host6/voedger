/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package storage

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/sys/checkpoints"
)

const (
	partitionRecoveryCheckpointPKeySize = 4 + 4 + 2
	workspaceRecoveryCheckpointPKeySize = 4 + 4 + 8
)

const (
	jsonFieldNextPLogOffset = "nextPLogOffset"
	jsonFieldNextWLogOffset = "nextWLogOffset"
	jsonFieldNextRecordID   = "nextRecordID"
)

type implRecoveryCheckpointStorage struct {
	sysVVMStorage ISysVvmStorage
}

func (s *implRecoveryCheckpointStorage) GetPartitionCheckpoint(appID istructs.ClusterAppID,
	partitionID istructs.PartitionID) (checkpoints.PartitionCheckpoint, bool, error) {
	data, ok, err := s.get(partitionRecoveryCheckpointPKey(appID, partitionID))
	if err != nil {
		return checkpoints.PartitionCheckpoint{}, false,
			fmt.Errorf("get partition recovery checkpoint: %w", err)
	}
	if !ok {
		return checkpoints.PartitionCheckpoint{}, false, nil
	}
	checkpoint, _, err := decodePartitionCheckpoint(data)
	if err != nil {
		return checkpoints.PartitionCheckpoint{}, false,
			fmt.Errorf("decode partition recovery checkpoint: %w", err)
	}
	return checkpoint, true, nil
}

func (s *implRecoveryCheckpointStorage) PutPartitionCheckpoint(appID istructs.ClusterAppID,
	partitionID istructs.PartitionID, checkpoint checkpoints.PartitionCheckpoint) error {
	pKey := partitionRecoveryCheckpointPKey(appID, partitionID)
	data, err := encodePartitionCheckpoint(checkpoint, nil)
	if err != nil {
		return fmt.Errorf("encode partition recovery checkpoint: %w", err)
	}
	if err := s.sysVVMStorage.Put(pKey, recoveryCheckpointCCols, data); err != nil {
		return fmt.Errorf("put partition recovery checkpoint: %w", err)
	}
	return nil
}

func (s *implRecoveryCheckpointStorage) GetWorkspaceCheckpoint(appID istructs.ClusterAppID,
	wsid istructs.WSID) (checkpoints.WorkspaceCheckpoint, bool, error) {
	data, ok, err := s.get(workspaceRecoveryCheckpointPKey(appID, wsid))
	if err != nil {
		return checkpoints.WorkspaceCheckpoint{}, false,
			fmt.Errorf("get workspace recovery checkpoint: %w", err)
	}
	if !ok {
		return checkpoints.WorkspaceCheckpoint{}, false, nil
	}
	checkpoint, _, err := decodeWorkspaceCheckpoint(data)
	if err != nil {
		return checkpoints.WorkspaceCheckpoint{}, false,
			fmt.Errorf("decode workspace recovery checkpoint: %w", err)
	}
	return checkpoint, true, nil
}

func (s *implRecoveryCheckpointStorage) PutWorkspaceCheckpoint(appID istructs.ClusterAppID,
	wsid istructs.WSID, checkpoint checkpoints.WorkspaceCheckpoint) error {
	pKey := workspaceRecoveryCheckpointPKey(appID, wsid)
	data, err := encodeWorkspaceCheckpoint(checkpoint, nil)
	if err != nil {
		return fmt.Errorf("encode workspace recovery checkpoint: %w", err)
	}
	if err := s.sysVVMStorage.Put(pKey, recoveryCheckpointCCols, data); err != nil {
		return fmt.Errorf("put workspace recovery checkpoint: %w", err)
	}
	return nil
}

func (s *implRecoveryCheckpointStorage) get(pKey []byte) ([]byte, bool, error) {
	var data []byte
	ok, err := s.sysVVMStorage.Get(pKey, recoveryCheckpointCCols, &data)
	return data, ok, err
}

func partitionRecoveryCheckpointPKey(appID istructs.ClusterAppID, partitionID istructs.PartitionID) []byte {
	pKey := make([]byte, 0, partitionRecoveryCheckpointPKeySize)
	pKey = binary.BigEndian.AppendUint32(pKey, pKeyPrefix_SeqStorage_Part)
	pKey = binary.BigEndian.AppendUint32(pKey, appID)
	return binary.BigEndian.AppendUint16(pKey, uint16(partitionID))
}

func workspaceRecoveryCheckpointPKey(appID istructs.ClusterAppID, wsid istructs.WSID) []byte {
	pKey := make([]byte, 0, workspaceRecoveryCheckpointPKeySize)
	pKey = binary.BigEndian.AppendUint32(pKey, pKeyPrefix_SeqStorage_WS)
	pKey = binary.BigEndian.AppendUint32(pKey, appID)
	return binary.BigEndian.AppendUint64(pKey, uint64(wsid))
}

func decodePartitionCheckpoint(data []byte) (checkpoints.PartitionCheckpoint, map[string]json.RawMessage, error) {
	fields, err := decodeCheckpointObject(data)
	if err != nil {
		return checkpoints.PartitionCheckpoint{}, nil, err
	}
	nextOffset, err := decodeRequiredJSONField[istructs.Offset](fields, jsonFieldNextPLogOffset)
	if err != nil {
		return checkpoints.PartitionCheckpoint{}, nil, err
	}
	return checkpoints.PartitionCheckpoint{NextPLogOffset: nextOffset}, fields, nil
}

func decodeWorkspaceCheckpoint(data []byte) (checkpoints.WorkspaceCheckpoint, map[string]json.RawMessage, error) {
	fields, err := decodeCheckpointObject(data)
	if err != nil {
		return checkpoints.WorkspaceCheckpoint{}, nil, err
	}
	nextWLogOffset, err := decodeRequiredJSONField[istructs.Offset](fields, jsonFieldNextWLogOffset)
	if err != nil {
		return checkpoints.WorkspaceCheckpoint{}, nil, err
	}
	nextRecordID, err := decodeRequiredJSONField[istructs.RecordID](fields, jsonFieldNextRecordID)
	if err != nil {
		return checkpoints.WorkspaceCheckpoint{}, nil, err
	}
	return checkpoints.WorkspaceCheckpoint{
		NextWLogOffset: nextWLogOffset,
		NextRecordID:   nextRecordID,
	}, fields, nil
}

func decodeCheckpointObject(data []byte) (map[string]json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, errors.New("recovery checkpoint must be a JSON object")
	}
	return fields, nil
}

func decodeRequiredJSONField[T ~uint64](fields map[string]json.RawMessage, name string) (T, error) {
	raw, ok := fields[name]
	if !ok {
		return 0, fmt.Errorf("required recovery checkpoint field %q is missing", name)
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, fmt.Errorf("decode recovery checkpoint field %q: %w", name, err)
	}
	return value, nil
}

func encodePartitionCheckpoint(checkpoint checkpoints.PartitionCheckpoint,
	fields map[string]json.RawMessage) ([]byte, error) {
	return encodeCheckpointObject(fields, map[string]any{
		jsonFieldNextPLogOffset: checkpoint.NextPLogOffset,
	})
}

func encodeWorkspaceCheckpoint(checkpoint checkpoints.WorkspaceCheckpoint,
	fields map[string]json.RawMessage) ([]byte, error) {
	return encodeCheckpointObject(fields, map[string]any{
		jsonFieldNextWLogOffset: checkpoint.NextWLogOffset,
		jsonFieldNextRecordID:   checkpoint.NextRecordID,
	})
}

func encodeCheckpointObject(fields map[string]json.RawMessage, values map[string]any) ([]byte, error) {
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	for name, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		fields[name] = encoded
	}
	return json.Marshal(fields)
}
