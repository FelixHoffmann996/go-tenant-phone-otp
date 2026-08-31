package otpservice

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrTenantNotFound  = errors.New("tenant not found")
	ErrAccountNotFound = errors.New("account not found")
	ErrAccountInactive = errors.New("account is not active")
	ErrCodeRejected    = errors.New("verification code rejected")
	ErrAdminRequired   = errors.New("tenant admin required")
	ErrInvalidInput    = errors.New("invalid input")
)

type AccountStatus string

const (
	AccountActive    AccountStatus = "active"
	AccountSuspended AccountStatus = "suspended"
	AccountClosed    AccountStatus = "closed"
)

type Account struct {
	Phone  string        `json:"phone"`
	Role   string        `json:"role"`
	Status AccountStatus `json:"status"`
}

type Tenant struct {
	ID       string              `json:"id"`
	Name     string              `json:"name"`
	Accounts map[string]*Account `json:"accounts"`
}

type CodeSender interface {
	Send(tenantID, phone, code string) error
}

type challenge struct {
	code      string
	expiresAt time.Time
}

type Service struct {
	mu         sync.Mutex
	captcha    CaptchaVerifier
	sender     CodeSender
	now        func() time.Time
	tenants    map[string]*Tenant
	challenges map[string]challenge
}

func NewService(captcha CaptchaVerifier, sender CodeSender) *Service {
	return &Service{
		captcha: captcha, sender: sender, now: time.Now,
		tenants: make(map[string]*Tenant), challenges: make(map[string]challenge),
	}
}

func (s *Service) OnboardTenant(ctx context.Context, name, adminPhone string, captcha CaptchaInput) (*Tenant, error) {
	if name == "" || adminPhone == "" {
		return nil, fmt.Errorf("%w: name and admin_phone are required", ErrInvalidInput)
	}
	if err := s.captcha.Verify(ctx, captcha); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := randomHex(8)
	if err != nil {
		return nil, err
	}
	tenant := &Tenant{ID: id, Name: name, Accounts: map[string]*Account{
		adminPhone: {Phone: adminPhone, Role: "admin", Status: AccountActive},
	}}
	s.tenants[id] = tenant
	return cloneTenant(tenant), nil
}

func (s *Service) AddAccount(tenantID, actorPhone, phone string) (*Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tenant, err := s.authorizedTenant(tenantID, actorPhone)
	if err != nil {
		return nil, err
	}
	account := &Account{Phone: phone, Role: "member", Status: AccountActive}
	tenant.Accounts[phone] = account
	copy := *account
	return &copy, nil
}

func (s *Service) SetAccountStatus(tenantID, actorPhone, phone string, status AccountStatus) error {
	if status != AccountActive && status != AccountSuspended && status != AccountClosed {
		return fmt.Errorf("%w: status must be active, suspended, or closed", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tenant, err := s.authorizedTenant(tenantID, actorPhone)
	if err != nil {
		return err
	}
	account := tenant.Accounts[phone]
	if account == nil {
		return ErrAccountNotFound
	}
	account.Status = status
	return nil
}

func (s *Service) SendLoginCode(ctx context.Context, tenantID, phone string, captcha CaptchaInput) error {
	if err := s.captcha.Verify(ctx, captcha); err != nil {
		return err
	}
	s.mu.Lock()
	tenant := s.tenants[tenantID]
	if tenant == nil {
		s.mu.Unlock()
		return ErrTenantNotFound
	}
	account := tenant.Accounts[phone]
	if account == nil {
		s.mu.Unlock()
		return ErrAccountNotFound
	}
	if account.Status != AccountActive {
		s.mu.Unlock()
		return ErrAccountInactive
	}
	code, err := numericCode()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.challenges[tenantID+"\x00"+phone] = challenge{code: code, expiresAt: s.now().Add(5 * time.Minute)}
	s.mu.Unlock()
	return s.sender.Send(tenantID, phone, code)
}

func (s *Service) VerifyLoginCode(tenantID, phone, code string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tenant := s.tenants[tenantID]
	if tenant == nil {
		return "", ErrTenantNotFound
	}
	account := tenant.Accounts[phone]
	if account == nil {
		return "", ErrAccountNotFound
	}
	if account.Status != AccountActive {
		return "", ErrAccountInactive
	}
	key := tenantID + "\x00" + phone
	entry, ok := s.challenges[key]
	delete(s.challenges, key)
	if !ok || !s.now().Before(entry.expiresAt) || subtle.ConstantTimeCompare([]byte(entry.code), []byte(code)) != 1 {
		return "", ErrCodeRejected
	}
	return randomHex(24)
}

func (s *Service) authorizedTenant(tenantID, actorPhone string) (*Tenant, error) {
	tenant := s.tenants[tenantID]
	if tenant == nil {
		return nil, ErrTenantNotFound
	}
	actor := tenant.Accounts[actorPhone]
	if actor == nil || actor.Role != "admin" || actor.Status != AccountActive {
		return nil, ErrAdminRequired
	}
	return tenant, nil
}

func cloneTenant(source *Tenant) *Tenant {
	copy := &Tenant{ID: source.ID, Name: source.Name, Accounts: make(map[string]*Account, len(source.Accounts))}
	for phone, account := range source.Accounts {
		accountCopy := *account
		copy.Accounts[phone] = &accountCopy
	}
	return copy
}

func randomHex(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate random value: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func numericCode() (string, error) {
	value := make([]byte, 4)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate login code: %w", err)
	}
	number := (uint32(value[0])<<24 | uint32(value[1])<<16 | uint32(value[2])<<8 | uint32(value[3])) % 1000000
	return fmt.Sprintf("%06d", number), nil
}
