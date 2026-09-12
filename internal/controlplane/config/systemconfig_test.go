package config

import (
	"context"
	"testing"
)

type systemConfigRepositoryStub struct{ writes int }

func (s *systemConfigRepositoryStub) GetSystemConfig(context.Context) (SystemConfig, error) {
	return SystemConfig{}, nil
}

func (s *systemConfigRepositoryStub) SetSystemConfig(_ context.Context, document SystemConfig, _ string) (SystemConfig, error) {
	s.writes++
	return document, nil
}

func TestSetSystemConfigRejectsNegativeFileMappingRetention(t *testing.T) {
	repository := &systemConfigRepositoryStub{}
	service := NewService(nil, nil, nil, nil, nil)
	service.SetSystemRepository(repository)
	if _, err := service.SetSystemConfig(context.Background(), SystemConfig{FileMappingRetentionDays: -1}, "actor"); err == nil {
		t.Fatal("negative retention was accepted")
	}
	if repository.writes != 0 {
		t.Fatalf("writes=%d", repository.writes)
	}
}

func TestValidateSystemConfigBoundsFileMappingRetention(t *testing.T) {
	for _, days := range []int{-1, MaxFileMappingRetentionDays + 1} {
		if err := ValidateSystemConfig(SystemConfig{FileMappingRetentionDays: days}); err == nil {
			t.Fatalf("retention %d was accepted", days)
		}
	}
	if err := ValidateSystemConfig(SystemConfig{FileMappingRetentionDays: MaxFileMappingRetentionDays}); err != nil {
		t.Fatalf("maximum retention rejected: %v", err)
	}
}
