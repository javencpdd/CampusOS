package space

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"time"
)

type avatarRetentionReceipt struct {
	Version    int       `json:"version"`
	FileName   string    `json:"file_name"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
	Mode       uint32    `json:"mode"`
	Digest     string    `json:"sha256"`
	Identity   string    `json:"identity"`
}

func avatarRetentionPath(store *LocalFileStore, owner, name string) (string, error) {
	if validateStorageSegment(owner) != nil || validateStorageSegment(name) != nil {
		return "", ErrSpaceFileInvalidName
	}
	dir, err := store.avatarDir(owner)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func readAvatarRetentionReceipt(ctx context.Context, store *LocalFileStore, owner, name string) (*avatarRetentionReceipt, error) {
	snapshot, err := avatarSelectionSnapshot(ctx, store, owner, name)
	if err != nil {
		return nil, err
	}
	path, err := avatarRetentionPath(store, owner, name)
	if err != nil {
		return nil, err
	}
	identity, err := avatarFileIdentity(path, snapshot.Info)
	if err != nil {
		return nil, errors.Join(ErrSpaceFactsUnavailable, err)
	}
	current, err := os.Lstat(path)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(snapshot.Info, current) || current.Size() != snapshot.Info.Size() || !current.ModTime().Equal(snapshot.Info.ModTime()) {
		return nil, ErrSpaceFactsUnavailable
	}
	digest := sha256.Sum256(snapshot.Image.Data)
	return &avatarRetentionReceipt{Version: 1, FileName: name, Size: current.Size(), ModifiedAt: current.ModTime().UTC(), Mode: uint32(current.Mode()), Digest: hex.EncodeToString(digest[:]), Identity: identity}, nil
}

func checkAvatarRetentionReceipt(ctx context.Context, store *LocalFileStore, owner string, expected *avatarRetentionReceipt) error {
	if expected == nil || expected.Version != 1 || expected.Size < 1 || expected.Size > store.MaxAvatarBytes() || validateStorageSegment(expected.FileName) != nil || len(expected.Digest) != 64 || expected.Identity == "" {
		return ErrSpaceFactsUnavailable
	}
	current, err := readAvatarRetentionReceipt(ctx, store, owner, expected.FileName)
	if err != nil || !reflect.DeepEqual(expected, current) {
		return errors.Join(ErrSpaceFactsUnavailable, err)
	}
	return nil
}

func avatarRetentionFileAbsent(store *LocalFileStore, owner, name string) (bool, error) {
	path, err := avatarRetentionPath(store, owner, name)
	if err != nil {
		return false, err
	}
	_, err = os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	return false, err
}
