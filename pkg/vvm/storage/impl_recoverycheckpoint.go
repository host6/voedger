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
	partitionRecoveryCheckpointPKeySize  = 4 + 4
	partitionRecoveryCheckpointCColsSize = 2
	workspaceRecoveryCheckpointPKeySize  = 4 + 4
	workspaceRecoveryCheckpointCColsSize = 8
)

const (
	jsonFieldLastHandledPLogOffset = "lastHandledPLogOffset"
	jsonFieldLastHandledWLogOffset = "lastHandledWLogOffset"
)

type implRecoveryCheckpointStorage struct {
	sysVVMStorage ISysVvmStorage
}

func (s *implRecoveryCheckpointStorage) GetPartitionCheckpoint(appID istructs.ClusterAppID,
	partitionID istructs.PartitionID) (checkpoints.PartitionCheckpoint, bool, error) {
	data, ok, err := s.get(partitionRecoveryCheckpointPKey(appID), partitionRecoveryCheckpointCCols(partitionID))
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
	pKey := partitionRecoveryCheckpointPKey(appID)
	data, err := encodePartitionCheckpoint(checkpoint)
	if err != nil {
		return fmt.Errorf("encode partition recovery checkpoint: %w", err)
	}
	if err := s.sysVVMStorage.Put(pKey, partitionRecoveryCheckpointCCols(partitionID), data); err != nil {
		return fmt.Errorf("put partition recovery checkpoint: %w", err)
	}
	return nil
}

func (s *implRecoveryCheckpointStorage) GetWorkspaceCheckpoint(appID istructs.ClusterAppID,
	wsid istructs.WSID) (checkpoints.WorkspaceCheckpoint, bool, error) {
	data, ok, err := s.get(workspaceRecoveryCheckpointPKey(appID), workspaceRecoveryCheckpointCCols(wsid))
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
	pKey := workspaceRecoveryCheckpointPKey(appID)
	data, err := encodeWorkspaceCheckpoint(checkpoint)
	if err != nil {
		return fmt.Errorf("encode workspace recovery checkpoint: %w", err)
	}
	if err := s.sysVVMStorage.Put(pKey, workspaceRecoveryCheckpointCCols(wsid), data); err != nil {
		return fmt.Errorf("put workspace recovery checkpoint: %w", err)
	}
	return nil
}

func (s *implRecoveryCheckpointStorage) get(pKey, cCols []byte) ([]byte, bool, error) {
	var data []byte
	ok, err := s.sysVVMStorage.Get(pKey, cCols, &data)
	return data, ok, err
}

func partitionRecoveryCheckpointPKey(appID istructs.ClusterAppID) []byte {
	pKey := make([]byte, 0, partitionRecoveryCheckpointPKeySize)
	pKey = binary.BigEndian.AppendUint32(pKey, pKeyPrefix_SeqStorage_Part)
	return binary.BigEndian.AppendUint32(pKey, appID)
}

func partitionRecoveryCheckpointCCols(partitionID istructs.PartitionID) []byte {
	cCols := make([]byte, 0, partitionRecoveryCheckpointCColsSize)
	return binary.BigEndian.AppendUint16(cCols, uint16(partitionID))
}

func workspaceRecoveryCheckpointPKey(appID istructs.ClusterAppID) []byte {
	pKey := make([]byte, 0, workspaceRecoveryCheckpointPKeySize)
	pKey = binary.BigEndian.AppendUint32(pKey, pKeyPrefix_SeqStorage_WS)
	return binary.BigEndian.AppendUint32(pKey, appID)
}

func workspaceRecoveryCheckpointCCols(wsid istructs.WSID) []byte {
	cCols := make([]byte, 0, workspaceRecoveryCheckpointCColsSize)
	return binary.BigEndian.AppendUint64(cCols, uint64(wsid))
}

func decodePartitionCheckpoint(data []byte) (checkpoints.PartitionCheckpoint, map[string]json.RawMessage, error) {
	fields, err := decodeCheckpointObject(data)
	if err != nil {
		return checkpoints.PartitionCheckpoint{}, nil, err
	}
	lastHandledPLogOffset, err := decodeRequiredJSONField[istructs.Offset](fields, jsonFieldLastHandledPLogOffset)
	if err != nil {
		return checkpoints.PartitionCheckpoint{}, nil, err
	}
	return checkpoints.PartitionCheckpoint{LastHandledPLogOffset: lastHandledPLogOffset}, fields, nil
}

func decodeWorkspaceCheckpoint(data []byte) (checkpoints.WorkspaceCheckpoint, map[string]json.RawMessage, error) {
	fields, err := decodeCheckpointObject(data)
	if err != nil {
		return checkpoints.WorkspaceCheckpoint{}, nil, err
	}
	lastHandledWLogOffset, err := decodeRequiredJSONField[istructs.Offset](fields, jsonFieldLastHandledWLogOffset)
	if err != nil {
		return checkpoints.WorkspaceCheckpoint{}, nil, err
	}
	return checkpoints.WorkspaceCheckpoint{
		LastHandledWLogOffset: lastHandledWLogOffset,
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

func encodePartitionCheckpoint(checkpoint checkpoints.PartitionCheckpoint) ([]byte, error) {
	return json.Marshal(checkpoint)
}

func encodeWorkspaceCheckpoint(checkpoint checkpoints.WorkspaceCheckpoint) ([]byte, error) {
	return json.Marshal(checkpoint)
}
