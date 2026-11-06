package workbench

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/zalando/go-keyring"
)

const modelKeyService = "org.databaseagent.model-api-key"

func (s *Store) modelKeyAccount(ref string) string {
	return fmt.Sprintf("%x/%s", sha256.Sum256([]byte(s.root)), ref)
}

func (s *Store) saveModelKey(key string) (string, error) {
	if len(key) > 2048 || strings.ContainsAny(key, "\r\n\x00") {
		return "", errors.New("API Key 必须为单行且不超过 2048 字节")
	}
	ref := id("key")
	if err := s.saveSecret(ref, key); err != nil {
		return ref, errors.New("无法保存 API Key，请确认系统凭据库可用且已解锁")
	}
	return ref, nil
}

func (s *Store) saveSecret(ref, value string) error {
	account := s.modelKeyAccount(ref)
	if err := keyring.Set(modelKeyService, account, value); err != nil {
		return err
	}
	// The macOS keyring command can exit successfully even if its write failed.
	if saved, err := keyring.Get(modelKeyService, account); err != nil || saved != value {
		return errors.New("credential store readback mismatch")
	}
	return nil
}

func (s *Store) deleteModelKey(ref string) {
	if ref == "" {
		return
	}
	if err := keyring.Delete(modelKeyService, s.modelKeyAccount(ref)); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		log.Print("无法从系统凭据库移除旧的模型 API Key")
	}
}

// modelWithKey resolves the agent's model under the read lock, then reads the
// keychain without it: an unlock prompt must not block every state write.
func (s *Store) modelWithKey(a Agent) (ModelProfile, string, error) {
	var m ModelProfile
	if err := s.Read(func(st *State) error {
		var err error
		m, err = modelFor(st, a)
		return err
	}); err != nil {
		return m, "", err
	}
	if m.APIKeyRef != "" {
		key, err := keyring.Get(modelKeyService, s.modelKeyAccount(m.APIKeyRef))
		if err != nil || key == "" {
			return m, "", errors.New("无法读取 API Key，请解锁系统凭据库或在偏好设置中重新填写密钥")
		}
		return m, key, nil
	}
	if m.APIKeyEnv != "" {
		key := os.Getenv(m.APIKeyEnv)
		if key == "" {
			return m, "", fmt.Errorf("环境变量 %s 未设置，请在偏好设置中填写 API Key 或设置该变量", m.APIKeyEnv)
		}
		return m, key, nil
	}
	return m, "", nil
}
