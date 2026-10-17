package secrets

import (
	"errors"
	"strings"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/zalando/go-keyring"
)

type Vault interface {
	Set(string) error
	Get() (string, error)
	Delete() error
}

type OSVault struct{}

const service = "go-fastly-ctrld"
const account = "controld-api-token"

func Validate(token string) error {
	if token == "" || token != strings.TrimSpace(token) || len(token) > 4096 || strings.ContainsAny(token, "\r\n\x00") {
		return domain.Fail("INVALID_INPUT", "權杖格式不正確")
	}
	return nil
}

func (OSVault) Set(token string) error {
	if err := Validate(token); err != nil {
		return err
	}
	if err := keyring.Set(service, account, token); err != nil {
		return domain.Fail("KEYRING_UNAVAILABLE", "無法寫入系統金鑰庫；未使用明文備援")
	}
	return nil
}

func (OSVault) Get() (string, error) {
	v, err := keyring.Get(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", domain.Fail("KEYRING_UNAVAILABLE", "無法讀取系統金鑰庫")
	}
	return v, nil
}

func (OSVault) Delete() error {
	err := keyring.Delete(service, account)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return domain.Fail("KEYRING_UNAVAILABLE", "無法刪除系統金鑰庫保存值")
	}
	return nil
}
